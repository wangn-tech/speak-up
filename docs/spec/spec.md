# AI 英语口语陪练平台 — 技术规格（Spec）

> 部分内容由豆包生成

# 0. 文档说明与契约纪律

本文档是《AI 英语口语陪练平台产品需求文档（PRD）》之后、三端开发之前的技术契约，主模式为 design_rfc（评审者可据此批准并实现未来技术状态）。范围：接口契约、数据设计、AI 编排、部署配置、验收与联调。读者：前端（React）、后端（Go）、AI 端（Python）研发与测试。

- **优先级：**与 PRD 冲突时，接口字段、表结构、事件 schema 以本文档为准（本地 [PRD](../prd/prd.md)）；
- **冻结流程：**v0.1 草案 → 评审 → v1.0 冻结；冻结后任何契约 / 表结构变更走第 7 章变更记录，不静默修改；
- **配置纪律：**全部外部配置（LLM / Embedding / ASR / TTS / 基础设施）经 .env 注入，代码不硬编码密钥；模板见 [`docs/.env.example`](../.env.example)；
- **标注约定：**本文档中【待评审】表示尚未冻结、需评审确认的内容；【待实测】表示依赖真实音频 / 计费验证后校准的内容。

> 本文档只定义"实现契约"，不重复产品行为与验收口径；产品行为见 PRD。接口 / 数据定义以本文档为唯一权威。

# 1. 范围与职责划分

## 1.1 服务边界

| 服务 | 技术栈 | 职责 | 对外协议 |
|-|-|-|-|
| Web 前端 | React + TypeScript | 门户 / 登录 / 主页面（工作台）/ 场景练习 / 语音对话 / 反馈与复盘展示 | 浏览器（REST + WebSocket） |
| 网关与后端 | Go + Gin + gRPC | 认证、业务 CRUD、WebSocket 会话网关与音频代理、Kafka 事件、业务指标 | REST + WebSocket（gin，端口 8080）；gRPC client → AI 服务（出站） |
| AI 服务 | Python + FastAPI + LangGraph（依赖管理：uv，pyproject.toml + uv.lock） | 在线 ReAct 会话、离线反馈 / 复盘 DAG、RAG 检索与记忆、ASR / TTS 编排、模型路由 | gRPC server（内部，供后端调用，端口 50051） |
| 基础设施 | MySQL / Redis / Kafka / Qdrant / Prometheus / Grafana | 业务数据 / 状态缓存 / 事件总线 / 向量库 / 可观测 | Docker Compose（本地与单机） |

## 1.2 关键取舍

- **音频流经后端网关代理**（不直连 AI 端）：统一鉴权、限流、断连恢复与审计；代价是多一跳延迟，MVP 单机部署可接受；
- **AI 端仅暴露 gRPC**（不直接对前端）：契约稳定、便于多端并行与模型路由；
- **事件总线选 Kafka**：异步管线（反馈 / 复盘 / 记忆写入）解耦与可重放；MVP 单 broker 即可；
- **向量库 Qdrant**（选型依据见 PRD 第 12 章）；主库 MySQL，不引入 pgvector。
- **后端单进程双协议**：gin（REST / WebSocket，面向前端，8080）与 gRPC client（面向 AI 服务，出站 50051）同进程共存，共享鉴权、领域逻辑与配置，避免多进程口径不一致；AI 端为 gRPC 服务提供方。

# 2. 接口契约（契约优先）

## 2.1 gRPC（后端 ↔ AI 服务）

**通信拓扑与方向：**AI 端为 gRPC 服务提供方（server，端口 50051），后端为 gRPC 客户端（client，内部网络调用）；离线结果（评估 / 复盘完成）经 Kafka 事件回写（evaluation.completed / review.completed），不做 gRPC 反向调用；后端单进程双协议见 1.1 / 1.2 / 5.1。

proto 独立仓库 / 版本管理（buf + go module + grpcio-tools 生成）；所有方法统一错误 envelope，见 2.1.4。

### 2.1.1 ConversationService（在线 ReAct 会话，双工流）

```protobuf
service ConversationService {
  rpc StreamChat(stream ClientEvent) returns (stream ServerEvent);
}

message ClientEvent {
  string session_id = 1; string user_id = 2; string scene_id = 3;
  uint32 turn_seq = 4; int64 ts_ms = 5;
  oneof payload {
    StartTurn start_turn = 10; AudioChunk audio_chunk = 11;
    EndTurn end_turn = 12; TextInput text_input = 13;
  }
}

message StartTurn { string audio_format=1; uint32 sample_rate=2; uint32 channels=3; }
message AudioChunk { bytes data=1; uint32 chunk_seq=2; }
message EndTurn {}
message TextInput { string text=1; }

message ServerEvent {
  string session_id = 1; string turn_id = 2;
  EventType type = 3;    // 见下方枚举
  string payload_json = 4;  // 各类型负载，见 2.1.1 事件负载表
  int64  ts_ms = 5;
}

enum EventType {
  UNKNOWN=0; ASR_PARTIAL=1; ASR_FINAL=2; REPLY_DELTA=3;
  TOOL_CALL=4; TOOL_RESULT=5; TTS_START=6; TTS_CHUNK=7;
  TURN_END=8; ERROR=9;
}
```

事件负载表：

| EventType | payload_json 字段 |
|-|-|
| ASR_PARTIAL / ASR_FINAL | {text, is_final} |
| REPLY_DELTA | {delta, intent, scene_action} |
| TOOL_CALL / TOOL_RESULT | {request_id, name, args_json} / {request_id, ok, data_json} |
| TTS_START / TTS_CHUNK | {voice} / {audio_base64, format: pcm\|mp3, sample_rate} |
| TURN_END | {turn_seq, duration_ms, usage_json} |
| ERROR | {code, message, retriable} |

### 2.1.2 RetrievalService（RAG 检索与记忆写入）

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

collection 取值：rag_knowledge（知识库）/ conv_memory（对话记忆）；默认 top_k=5。

### 2.1.3 EvaluationService / ReviewService（离线 DAG 任务）

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

### 2.1.4 统一错误码

| code | 含义 | retriable | 建议动作 |
|-|-|-|-|
| ERR_LLM_TIMEOUT | LLM 调用超时 | true | 重试一次 → 降级模型 → 引导语 |
| ERR_ASR_TIMEOUT / ERR_TTS_TIMEOUT | 语音服务超时 | true | 提示用户重试，本轮作废 |
| ERR_RAG_UNAVAILABLE | 向量库不可用 | true | 无检索直出，响应带无引用标记 |
| ERR_TASK_DUPLICATED | event_id 重复 | false | 幂等返回已有结果 |
| ERR_SCENE_INVALID | 场景状态非法 | false | 结束会话并提示 |
| ERR_RATE_LIMITED | 限流 | true | 退避重试 |

统一 envelope：{code, message, retriable}，gRPC 与 REST 共用同一错误体系。

## 2.2 REST（前端 ↔ 后端）

| 方法 + 路径 | 说明 |
|-|-|
| POST /api/v1/auth/register | 注册（手机号 + 验证码） |
| POST /api/v1/auth/login | 登录，返回 JWT |
| POST /api/v1/auth/refresh | 刷新令牌 |
| GET /api/v1/scenes | 场景列表（分页） |
| GET /api/v1/scenes/{id} | 场景详情 |
| POST /api/v1/sessions | 创建会话 |
| GET /api/v1/sessions | 当前用户会话历史（分页） |
| POST /api/v1/sessions/{id}/end | 结束会话（触发反馈/复盘事件） |
| GET /api/v1/sessions/{id} | 会话详情（含回合列表） |
| GET /api/v1/evaluations?session_id= | 查询评估结果 |
| GET /api/v1/reviews/{id} | 复盘报告 |
| GET / PUT /api/v1/profiles/me | 个人画像读写 |

统一响应 envelope：{code, msg, data}；鉴权 Bearer JWT；分页统一 ?page=&page_size=；错误码与 2.1.4 对齐。

## 2.3 WebSocket 音频协议（前端 ↔ 网关 /ws/conversation）

### 2.3.1 连接

```text
wss://<host>/ws/conversation?token=<JWT>&session_id=<id>
```

### 2.3.2 帧格式

- **上行二进制帧：**PCM 16kHz / 16bit / 单声道，每帧 ≤ 200ms；
- **上行文本帧：**JSON {type, payload}，type ∈ start | audio_start | audio_end | text | end | interrupt | heartbeat；
- **下行文本帧：**JSON {type, payload}，type ∈ asr_partial | asr_final | reply_delta | tool_event | tts_start | tts_chunk | turn_end | error | pong；tts_chunk.payload 为 base64 音频。

### 2.3.3 会话时序（MVP 主路径）

1. 客户端发送 start → 服务端确认会话可用；
2. 客户端发送 audio_start → 用户语音（二进制帧）→ audio_end；服务端透传 AI 端 ASR，下行 asr_partial / asr_final；文字模式发送 text；
3. AI 端流式回复：reply_delta（文本）+ tool_event（可选透传）→ tts_start → tts_chunk（音频帧）；
4. turn_end 结束本轮，客户端可发起下一轮；
5. 客户端发送 end 或服务端空闲超时 → 会话结束落库。

### 2.3.4 打断（P1 后置）

协议预留 interrupt 事件；MVP 阶段服务端返回 error {code: ERR_INTERRUPT_UNSUPPORTED}，前端可忽略。打断的 VAD 判定与 TTS 停流在 P1 实现。【待评审】

### 2.3.5 心跳与超时

客户端每 30s 发 heartbeat；服务端 60s 无任何帧则判定超时，关闭连接并将会话置为异常态（可恢复 / 可重连）。

## 2.4 Kafka 事件 schema（内部总线）

| topic | 生产者 / 消费者 | 关键 payload 字段 |
|-|-|-|
| session.events | 后端 → AI / 落库 | {event_id, session_id, user_id, type, ts} |
| evaluation.trigger | 后端 → AI 反馈 DAG | {event_id, session_id, user_id, transcript_uri, audio_uri} |
| review.trigger | 后端 → AI 复盘 DAG | {event_id, user_id, session_id, period_start, period_end} |
| evaluation.completed / review.completed | AI → 后端 | {event_id, task_id, session_id, user_id, status, result_json, schema_version} |
| memory.write | AI → 后端（归档） | {event_id, user_id, content, kind} |

通用头字段 event_id（uuid）用于幂等；分区键取 session_id 保证同会话保序；消息带 schema_version。

# 3. 数据设计（DDL 与键位）

## 3.1 MySQL DDL 草案（首版）

```sql
CREATE TABLE users (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  phone VARCHAR(20) NOT NULL UNIQUE, password_hash VARCHAR(128) NOT NULL,
  nickname VARCHAR(64), avatar_url VARCHAR(255), status TINYINT DEFAULT 1,
  created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);

CREATE TABLE scenes (
  id BIGINT AUTO_INCREMENT PRIMARY KEY, code VARCHAR(64) NOT NULL UNIQUE,
  title VARCHAR(128) NOT NULL, description TEXT, level VARCHAR(16),
  type VARCHAR(32), config_json JSON, status TINYINT DEFAULT 1, sort INT);

CREATE TABLE sessions (
  id BIGINT AUTO_INCREMENT PRIMARY KEY, user_id BIGINT NOT NULL, scene_id BIGINT NOT NULL,
  status VARCHAR(16) NOT NULL,            -- active | finished | aborted
  started_at DATETIME, ended_at DATETIME, duration_sec INT,
  transcript_uri VARCHAR(512), audio_uri VARCHAR(512),
  review_status VARCHAR(16) DEFAULT 'pending',   -- pending | done | failed
  KEY idx_user_time (user_id, started_at));

CREATE TABLE turns (
  id BIGINT AUTO_INCREMENT PRIMARY KEY, session_id BIGINT NOT NULL, seq INT NOT NULL,
  user_text TEXT, asr_text TEXT, assistant_text TEXT,
  intent VARCHAR(32), scene_action VARCHAR(32),
  ts_start BIGINT, ts_end BIGINT, audio_uri VARCHAR(512),
  KEY idx_session_seq (session_id, seq));

CREATE TABLE evaluations (
  id BIGINT AUTO_INCREMENT PRIMARY KEY, session_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
  status VARCHAR(16), overall_score DECIMAL(4,1),
  dimensions_json JSON, issues_json JSON, suggestions_json JSON,
  created_at DATETIME, KEY idx_session (session_id));

CREATE TABLE reviews (
  id BIGINT AUTO_INCREMENT PRIMARY KEY, user_id BIGINT NOT NULL,
  period_start DATE, period_end DATE, report_json JSON,
  status VARCHAR(16), created_at DATETIME,
  KEY idx_user_period (user_id, period_end));

CREATE TABLE user_profiles (
  id BIGINT AUTO_INCREMENT PRIMARY KEY, user_id BIGINT NOT NULL UNIQUE,
  level VARCHAR(16), goals_json JSON, weak_points_json JSON, updated_at DATETIME);
```

## 3.2 Redis key 设计

| key | 类型 | TTL | 用途 |
|-|-|-|-|
| session:state:{session_id} | Hash | 会话存续 | 场景状态机、turn_seq |
| task:lock:{task_type}:{event_id} | String SETNX | 10 min | 异步任务幂等锁 |
| rate:user:{user_id} | 计数 | 窗口期 | 接口限流 |
| cache:scene:{scene_id} | String JSON | 1 h | 场景配置缓存 |

## 3.3 Qdrant collection

| collection | 维度 / 距离 | payload 字段（全部可过滤） |
|-|-|-|
| rag_knowledge | 1024 / Cosine | {user_id 可空（空 = 全局）, scene_id, content_type, source, event_id, created_at} |
| conv_memory | 1024 / Cosine | {user_id 必填, session_id, turn_id, kind（profile / event / mistake）, event_id, created_at} |

写入幂等：point id = event_id，使用 upsert；检索过滤按 user_id + scene_id，保证用户级隔离；维度必须与 EMBEDDING_DIMENSIONS 一致（1024）。

# 4. AI 编排设计（LangGraph）

## 4.1 图状态 Schema

```python
class GraphState(TypedDict):
    user_id: str
    session_id: str
    scene_id: str
    scene_state: dict            # 场景状态机位置
    turn_seq: int
    short_memory: list           # 最近 10 轮摘要
    retrieved_chunks: list       # RAG 结果
    tool_steps: int              # ReAct 循环计数
    reply: dict                  # {text, intent, scene_action, safety}
    task_id: str                 # 离线任务标识
```

## 4.2 ReAct 图（在线会话，角色扮演 / 意图识别）

节点与边：入口 → 意图路由（轻量 LLM，低置信默认「继续剧情」）→ ReAct 循环 {Thought → ToolNode → Observation}（最大步数 ≤ 3）→ 回复生成 → 出口事件流。

### 4.2.1 工具协议

```text
tool_call:  {name, args_json, request_id}
tool_result:{request_id, ok, data_json}
工具清单：rag_search | memory_read | memory_write | safety_check | dict_lookup
约束：每次工具调用超时 3s，重试 1 次；循环总步数 ≤ 3；超限截断并输出当前最优回复。
```

### 4.2.2 降级策略

- LLM 超时：重试一次 → 降级模型（qwen3.6-flash）→ 仍失败输出通用引导语；
- RAG 不可用：无检索直出，回复不引用任何语料；
- 内容安全拦截：替换为安全回复并记录事件（不改写原意）。

## 4.3 反馈 DAG（离线异步）

入口 → 并行分支 {发音评测 / 语法纠错 / 流畅度 / 内容质量} → 汇合 reduce → 评分聚合 → 结构化评估输出（dimensions / issues / suggestions）→ 写库（幂等 event_id）。

失败处理：单节点失败 → 该维度标记「待补充」，不阻塞聚合；全部节点失败 → 任务 failed 并告警；任务由 evaluation.trigger 事件驱动，事件消费幂等（task:lock）。

## 4.4 复盘 DAG（离线异步）

证据收集（conv_memory 检索 + 本次评估）→ 并行 {趋势分析 / 错题提取 / 学习建议} → 汇合 → 报告生成 → 落库 + 通知（review.completed）。

## 4.5 模型路由表

| 任务 | 模型 | 备注 |
|-|-|-|
| 在线对话回合 | qwen3.7-plus | 平衡档（旗舰 qwen3.8-max / 经济 qwen3.6-flash） |
| 意图识别 | qwen3.6-flash | 经济档 |
| 离线评测 / 复盘 | deepseek-v4-pro（走百炼） | 深度推理 |
| Embedding | text-embedding-v4 | 1024 维 |

模型路由与参数经 LLM 统一网关（限流 / 超时 / 降级 / 成本统计）；模型命名注意 PRD 第 12 章：旧名（qwen-max/plus、deepseek-chat/reasoner、deepseek-v3/r1）已弃用或即将下架。

# 5. 部署与配置

## 5.1 docker-compose 服务拓扑

| 服务 | 镜像 / 技术 | 端口 | 依赖 |
|-|-|-|-|
| web | nginx（静态 + 反代） | 80 / 443 | gateway, ai |
| gateway | Go 后端 | 8080（gin REST/WS）, 9091(metrics)；gRPC client 出站 50051 | mysql, redis, kafka, ai |
| ai | Python FastAPI（uv 依赖管理） | 50051（gRPC server）, 9092(metrics) | kafka, qdrant, 外部 API |
| mysql / redis / kafka / qdrant / prometheus / grafana | 官方镜像 | 常规端口 | — |

## 5.2 .env 键位落实

全部键见 [`docs/.env.example`](../.env.example)；键位消费方约束：LLM / Embedding / ASR / TTS / Qdrant 键由 AI 端读取；MYSQL / REDIS / KAFKA / 可观测键由网关与 AI 端按需读取；新增键必须同步本文档与该模板。

## 5.3 监控指标清单

| 指标 | 来源 | 类型 |
|-|-|-|
| http_requests_total / http_latency_seconds / http_errors_total | Go | Counter / Histogram |
| ws_active_connections / ws_message_latency_seconds | Go | Gauge / Histogram |
| agent_step_latency_seconds / tool_latency_seconds / llm_tokens_total / dag_node_failure_total | AI | Histogram / Counter |
| kafka_consumer_lag | Kafka exporter | Gauge |

Grafana 基础看板：QPS、延迟分位（p50 / p95 / p99）、错误率、Kafka 积压、token 消耗与成本、Agent 步数分布；告警规则覆盖错误率 / 延迟 / 积压 / 复盘任务失败率。

# 6. 验收与联调

## 6.1 契约测试清单

- proto：buf lint + 每个 rpc 的请求 / 响应样例契约测试；
- REST：OpenAPI 校验 + 鉴权 / 限流用例；
- WebSocket：回声测试、帧格式校验、超时断开、断线重连；
- Kafka：schema 校验 + 重复 event_id 消费幂等用例。

## 6.2 联调顺序

1. 基础 REST（auth / scenes / sessions）三端打通；
2. AI 端 LLM 流式自测（qwen + deepseek 双通道）；
3. gRPC 打通（Conversation / Retrieval）；
4. WebSocket 全链路：音频进 → ASR → LLM → TTS 出；
5. 离线 DAG（反馈 / 复盘）异步落库 + 通知；
6. 可观测面板与告警规则验证。

## 6.3 异常用例（MVP 必测）

| 场景 | 预期行为（验收标准） |
|-|-|
| ASR 超时 | 提示用户重试，不产生回合 |
| LLM 超时 | 重试 → 降级模型 → 引导语，触发可观测告警 |
| RAG 不可用 | 无检索直出，回复带无引用标记 |
| event_id 重复 | 幂等返回已有结果，不重复计分 / 不重复写入 |
| DAG 单节点失败 | 该维度「待补充」，管线完成 |
| Kafka 消费重复 | 消费端幂等，结果不重复写入 |
| 会话中途断连 | 会话置异常态，可恢复 / 可重连 |

# 7. 变更记录与冻结

| 版本 | 日期 | 变更 | 状态 |
|-|-|-|-|
| v0.1 | 2026-09-10 | 初稿：接口契约 / 数据设计 / AI 编排 / 部署 / 验收（契约先行） | 草案【待评审】 |

冻结流程：三端评审通过后升 v1.0；此后任何接口 / 表结构 / 事件 schema 变更须新增版本行，说明变更内容与影响范围，评审后生效。
