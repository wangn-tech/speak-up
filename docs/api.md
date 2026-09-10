# AI 英语口语陪练平台 — 接口文档（API）

> 部分内容由豆包生成

# 0. 文档说明与契约地位

本文档是《AI 英语口语陪练平台 — 技术规格（Spec）》第 2 章「接口契约」的展开实现文档，为三端研发与测试提供可直接对着编码、联调与验收的接口参考手册。接口字段、事件 schema 与错误码以本文档为唯一权威；与 Spec / PRD 冲突时按 Spec 第 0 章优先级裁决。

- **上游文档：**本地 [PRD](prd/prd.md)；本地 [Spec](spec/spec.md)；
- **标注约定：**【待评审】= 尚未冻结、需评审确认；【待实测】= 依赖真实音频 / 计费验证后校准；
- **冻结流程：**随 Spec 一同评审，Spec v1.0 冻结时本文档同步冻结；冻结后变更走本文档第 6 章变更记录，不静默修改；
- **Base URL 约定：**REST 前缀 /api/v1（gin，8080）；WebSocket wss://<host>/ws/conversation；gRPC 仅内网（AI 端 server，50051）。

> 本文档只描述「接口长什么样」，不重复产品行为与验收口径；产品行为见 PRD，架构与数据见 Spec。

# 1. 通用约定

## 1.1 鉴权

| 项 | 约定 |
|-|-|
| 机制 | JWT（Bearer），登录后签发 access_token 与 refresh_token |
| 有效期（暂定） | access_token 2h，refresh_token 7d【待评审】 |
| 请求头 | Authorization: Bearer <access_token> |
| 刷新 | POST /api/v1/auth/refresh，携带 refresh_token，返回新 access_token |
| WebSocket | 连接参数 token=<access_token>（见第 4 章） |

## 1.2 统一响应 envelope

```json
{ "code": 0, "msg": "ok", "data": { } }
```

- **code=0** 表示成功，非 0 为业务错误码（见 1.3）；HTTP 状态码仅表达传输层结果（200 成功、401 未认证、403 无权限、429 限流、5xx 服务异常）；
- **分页：**统一 ?page=1&page_size=20，响应 data = { "page": 1, "page_size": 20, "total": 123, "list": [] }；
- **时间：**int64 毫秒时间戳（ts_ms）；所有字符串 UTF-8；
- **幂等：**离线任务与记忆写入必须携带 event_id（uuid），服务端去重（见 2.3 / 2.2 / 5.2）。

## 1.3 统一错误码

| code | 含义 | retriable | 建议动作 |
|-|-|-|-|
| ERR_LLM_TIMEOUT | LLM 调用超时 | true | 重试一次 → 降级模型 → 引导语 |
| ERR_ASR_TIMEOUT | ASR 调用超时 | true | 提示用户重试，本轮作废 |
| ERR_TTS_TIMEOUT | TTS 调用超时 | true | 提示用户重试，本轮作废 |
| ERR_RAG_UNAVAILABLE | 向量库不可用 | true | 无检索直出，响应带无引用标记 |
| ERR_TASK_DUPLICATED | event_id 重复 | false | 幂等返回已有结果 |
| ERR_SCENE_INVALID | 场景状态非法 | false | 结束会话并提示 |
| ERR_RATE_LIMITED | 限流 | true | 退避重试（429） |
| ERR_INTERRUPT_UNSUPPORTED | 打断暂不支持（MVP） | false | 前端忽略，见 4.5 |

统一 envelope：{code, message, retriable}，gRPC 与 REST 共用同一错误体系。

# 2. gRPC 接口（后端 ↔ AI 端）

**拓扑与方向：**AI 端为 gRPC 服务提供方（server，端口 50051），后端为 gRPC 客户端（client，仅内网调用）；离线结果回写走 Kafka 事件，不做 gRPC 反向调用。proto 独立仓库 / 版本管理（buf + go module + grpcio-tools 生成）。

## 2.1 ConversationService — 在线对话双工流

```protobuf
service ConversationService {
  rpc StreamChat(stream ClientTurn) returns (stream ServerEvent);
}

message ClientTurn {
  string session_id = 1; string user_id = 2; string scene_id = 3;
  uint32 turn_seq = 4;   string text = 5;            // 客户端文本（备用）
  repeated TranscriptChunk transcript = 6;           // ASR 转写片段
  int64  ts_ms = 7;
}

message ServerEvent {
  string session_id = 1; string turn_id = 2;
  EventType type = 3;
  string payload_json = 4;  // 各类型负载见事件负载表
  int64  ts_ms = 5;
}

enum EventType {
  UNKNOWN=0; ASR_PARTIAL=1; ASR_FINAL=2; REPLY_DELTA=3;
  TOOL_CALL=4; TOOL_RESULT=5; TTS_START=6; TTS_CHUNK=7;
  TURN_END=8; ERROR=9;
}
```

### 事件负载表

| EventType | payload_json 字段 |
|-|-|
| ASR_PARTIAL / ASR_FINAL | {text, is_final} |
| REPLY_DELTA | {delta, intent, scene_action} |
| TOOL_CALL / TOOL_RESULT | {request_id, name, args_json} / {request_id, ok, data_json} |
| TTS_START / TTS_CHUNK | {voice} / {audio_base64, format: pcm\|mp3, sample_rate} |
| TURN_END | {turn_seq, duration_ms, usage_json} |
| ERROR | {code, message, retriable} |

### 流生命周期

1. 后端以 turn_seq 递增顺序发送 ClientTurn（首个必须含 session_id / user_id / scene_id）；
2. AI 端按序回 ServerEvent；同一轮内 REPLY_DELTA 可多帧，TTS_START → TTS_CHUNK\* → TURN_END 顺序固定；
3. TURN_END 后本轮结束，后端可发送下一轮 ClientTurn；
4. 任一侧发送 EOF 即关闭流；ERROR 帧后 AI 端关闭流（retriable=true 时后端可重试）。

## 2.2 RetrievalService — RAG 检索与记忆写入

```protobuf
service RetrievalService {
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc Store(StoreRequest) returns (StoreAck);
}
message SearchRequest { string user_id=1; string query=2; string collection=3; uint32 top_k=4; string filters_json=5; float min_score=6; }
message SearchResponse { repeated Chunk chunks=1; }
message Chunk { string id=1; string text=2; float score=3; string payload_json=4; }
message StoreRequest { string event_id=1; string user_id=2; string collection=3; string text=4; string metadata_json=5; }
message StoreAck { bool ok=1; bool duplicated=2; }  // duplicated=true 表示 event_id 已存在，未重复写入
```

- **collection** 取值：rag_knowledge（知识库）/ conv_memory（对话记忆）；默认 top_k=5，min_score 默认 0.0；
- **Search 降级：**向量库不可用时返回 ERR_RAG_UNAVAILABLE，调用方降级为无检索直出；
- **Store 幂等：**以 event_id 去重（对应 Kafka memory.write 事件），重复返回 duplicated=true 且不覆盖。

## 2.3 EvaluationService / ReviewService — 离线 DAG 任务

```protobuf
service EvaluationService {
  rpc Submit(EvalTask) returns (TaskAck);       // 触发反馈 DAG
  rpc Get(EvalQuery) returns (EvalResult);       // 查询
}
message EvalTask { string event_id=1; string session_id=2; string user_id=3; string transcript_uri=4; string audio_uri=5; string timestamps_json=6; }
message TaskAck { string task_id=1; bool accepted=2; bool duplicated=3; }
message EvalQuery { string session_id=1; string user_id=2; }
message EvalResult { string task_id=1; string status=2; string result_json=3; }

service ReviewService {
  rpc Submit(ReviewTask) returns (TaskAck);     // 触发复盘 DAG
  rpc Get(ReviewQuery) returns (ReviewResult);
}
```

- **状态机：**pending → running → done | failed；failed 可重提（重新 Submit 产生新 task_id）；
- **幂等：**event_id 重复时 accepted=false、duplicated=true，返回已有 task_id；
- **回写：**任务完成由 AI 端发 Kafka evaluation.completed / review.completed 事件（见第 5 章），后端据此落库并推送前端；
- **Get 查询**供后端轮询 / 补偿用（MVP 主路径走 Kafka 回写）。

## 2.4 错误 envelope 与超时

gRPC 统一返回错误：{code, message, retriable}（code 见 1.3 表）；后端侧建议超时：StreamChat 首事件 5s / 整体 60s【待实测】；Search / Store / Submit / Get 3s。

# 3. REST 接口（前端 ↔ 后端）

统一前缀 /api/v1，鉴权 Bearer JWT，响应 envelope 见 1.2；以下为 v0.1 全部接口。【待评审】

## 3.1 认证

| 方法 + 路径 | 鉴权 | 说明 |
|-|-|-|
| POST /auth/register | 无 | 注册：{phone, sms_code, nickname?, password?} |
| POST /auth/login | 无 | 登录：{phone, sms_code} 或 {phone, password}，返回 {access_token, refresh_token, user} |
| POST /auth/refresh | refresh_token | 刷新：{refresh_token} → {access_token, refresh_token} |

```json
// POST /api/v1/auth/login 请求
{ "phone": "13800000000", "sms_code": "123456" }
// 响应
{ "code": 0, "msg": "ok", "data": {
  "access_token": "eyJ...", "refresh_token": "eyJ...",
  "user": { "user_id": "u_1001", "nickname": "Ada", "level": "A2" } } }
```

## 3.2 场景

| 方法 + 路径 | 说明与关键字段 |
|-|-|
| GET /scenes | 场景列表（分页）：返回 [{scene_id, title, difficulty, category, cover_uri, description}] |
| GET /scenes/{id} | 场景详情：{scene_id, title, difficulty, objective, roles, outline_steps[], tips[], starter_prompt} |

## 3.3 会话

| 方法 + 路径 | 说明与关键字段 |
|-|-|
| POST /sessions | 创建会话：{scene_id, mode?} → {session_id, ws_token, ws_url, expires_in} |
| POST /sessions/{id}/end | 结束会话：触发评估与复盘事件（Kafka evaluation.trigger / review.trigger） |
| GET /sessions/{id} | 会话详情：{session_id, scene, status, started_at, ended_at, turns[], transcript_uri} |

```json
// POST /api/v1/sessions 响应
{ "code": 0, "msg": "ok", "data": {
  "session_id": "s_20260910_0001",
  "ws_url": "wss://host/ws/conversation",
  "ws_token": "eyJ...", "expires_in": 120 } }
```

## 3.4 评估与复盘

| 方法 + 路径 | 说明与关键字段 |
|-|-|
| GET /evaluations?session_id= | 评估结果列表：[{evaluation_id, session_id, status, overall_score, dimensions{fluency, accuracy, pronunciation, vocabulary, interaction}, comment, created_at}] |
| GET /reviews/{id} | 复盘报告：{review_id, user_id, period_start, period_end, stats, highlights[], weak_points[], recommendations[], history[]} |

## 3.5 个人画像

| 方法 + 路径 | 说明与关键字段 |
|-|-|
| GET /profiles/me | 读画像：{level, goals, weak_tags[], learning_history[], preferred_scenes[]} |
| PUT /profiles/me | 写画像：支持部分字段更新；画像由 AI 端 DAG 增量更新，用户可覆盖 |

# 4. WebSocket 音频协议（前端 ↔ 网关）

## 4.1 连接与鉴权

```text
wss://<host>/ws/conversation?token=<JWT>&session_id=<id>
```

token 为创建会话返回的 ws_token（短时效）；鉴权失败返回 error {code: ERR_AUTH, message} 并关闭。

## 4.2 帧格式

| 方向 | 帧类型 | 格式 |
|-|-|-|
| 上行 | 二进制帧 | PCM 16kHz / 16bit / 单声道，每帧 ≤ 200ms |
| 上行 | 文本帧 | JSON {type, payload}，type ∈ start \| end \| interrupt \| heartbeat |
| 下行 | 文本帧 | JSON {type, payload}，type ∈ asr_partial \| asr_final \| reply_delta \| tool_event \| tts_start \| tts_chunk \| turn_end \| error \| pong；tts_chunk.payload 为 base64 音频 |

## 4.3 会话时序（MVP 主路径）

1. 客户端发送 start → 服务端确认会话可用；
2. 用户语音（二进制帧）→ 服务端透传 AI 端 ASR，下行 asr_partial / asr_final；
3. AI 端流式回复：reply_delta（文本）+ tool_event（可选透传）→ tts_start → tts_chunk（音频帧）；
4. turn_end 结束本轮，客户端可发起下一轮；
5. 客户端发送 end 或服务端空闲超时 → 会话结束落库。

## 4.4 心跳与超时

客户端每 30s 发 heartbeat；服务端 60s 无任何帧判定超时，关闭连接并将会话置为异常态（可恢复 / 可重连，恢复后按 turn_seq 续传）。

## 4.5 打断（P1 后置）

协议预留 interrupt 事件；MVP 阶段服务端返回 error {code: ERR_INTERRUPT_UNSUPPORTED}，前端可忽略。打断的 VAD 判定与 TTS 停流在 P1 实现。【待评审】

## 4.6 错误与断线重连

- 服务端主动关闭前必须发送 error 帧（code 见 1.3）再关闭；
- 网络闪断：客户端按指数退避重连（1s / 2s / 4s，最多 3 次），重连后发 start 续传；
- 会话状态落 Redis（见 Spec 3.2），后端可恢复未完成回合。

# 5. Kafka 事件（内部总线）

## 5.1 topic 与方向

| topic | 生产者 → 消费者 | 关键 payload 字段 |
|-|-|-|
| session.events | 后端 → AI / 落库 | {event_id, session_id, user_id, type, ts} |
| evaluation.trigger | 后端 → AI 反馈 DAG | {event_id, session_id, user_id, transcript_uri, audio_uri} |
| review.trigger | 后端 → AI 复盘 DAG | {event_id, user_id, session_id, period_start, period_end} |
| evaluation.completed / review.completed | AI → 后端 | {task_id, status, result_uri} |
| memory.write | AI → 后端（归档） | {event_id, user_id, content, kind} |

## 5.2 通用头与分区

- **event_id（uuid）：**全局唯一，用于消费者幂等（消费后落去重表，重复直接 ACK）；
- **分区键：**session_id，保证同会话事件有序消费；
- **schema_version：**消息必带，消费者按版本兼容；
- **重试：**消费失败进重试 topic（后缀 .retry），最多 3 次，仍失败进 .dlq 告警。

# 6. 版本与变更记录

| 版本 | 日期 | 变更内容 |
|-|-|-|
| v0.1 | 2026-09-10 | 初始稿【待评审】：gRPC 3 服务 6 方法、REST 10 接口、WS 帧协议、Kafka 5 topic；随 Spec v0.1 评审。 |

冻结后任何接口 / 字段 / 事件 schema 变更须新增版本行，并同步 Spec 第 7 章与三端代码生成产物。
