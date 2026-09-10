# SpeakUp 开发指南

## 当前状态

SpeakUp 是 Web 端 AI 英语口语陪练，核心闭环为场景对话、实时语音、个性化反馈和周期复盘。

仓库当前处于设计阶段，只有 `docs/` 与 CI 配置；`frontend/`、`backend/`、`ai/`、`proto/`、`docker/` 是 Spec 中的目标结构，尚未落地。执行命令或引用路径前先检查文件是否存在，不要把规划内容当成已有实现。

## 权威资料

| 文件 | 负责内容 |
|-|-|
| `docs/prd/prd.md` | 产品范围、用户行为、优先级、验收口径 |
| `docs/spec/spec.md` | 服务边界、架构、数据、AI 编排、部署与联调 |
| `docs/api/api.md` | REST、WebSocket、gRPC、Kafka 的详细接口约定 |
| `docs/.env.example` | 配置键清单；只放占位值，不提交真实密钥 |
| `.github/workflows/ci.yml` | 实际 CI 质量门 |

按内容所属领域使用对应文档；跨文档冲突由 Spec 裁决。三份文档目前均为 v0.1 草案：保留【待评审】和【待实测】标记，不擅自冻结结论。将来加入 `.proto` 后，以其为可执行的 gRPC 契约，并同步 Spec、API 和生成代码。

## 架构约束

- 前端只连接 Go 网关：REST `/api/v1/*` 和 WebSocket `/ws/conversation`。
- 网关负责鉴权、业务数据、WebSocket 音频代理和 Kafka 事件，并作为 gRPC client 调用 AI 服务。
- AI 服务只在内网提供 gRPC（目标端口 `50051`），承载 LLM、ASR、TTS、RAG 与 LangGraph；不得直接暴露给前端。
- 在线会话使用有步数上限的 ReAct 图；反馈与复盘使用 Kafka 驱动的离线 DAG，并以 `event_id` 保证幂等。

## 修改规则

- 契约优先：接口或事件变更同步 API、Spec、`.proto` 与相关测试，并更新文档变更记录；不要静默修改已冻结契约。
- 生成代码随仓库提交，但不得手改 `backend/gen/` 或 `ai/gen/`；修改 `.proto` 后重新运行 `buf generate`。
- 外部配置只从环境变量读取。新增或重命名配置时同步 `docs/.env.example` 与 Spec 5.2。
- 遵守服务边界，不在前端放供应商密钥或直连 AI 服务，不让 AI 服务反向调用网关回写离线结果。
- 只改任务涉及的文件；保留现有未提交改动。依赖变更必须同步锁文件。

## CI 质量门

CI 当前无条件运行四个 job，因此首次实现必须补齐四个服务目录及其依赖清单，不能通过跳过 job 掩盖缺失脚手架。命令均在对应目录执行：

| 目录 | 必须通过的命令 |
|-|-|
| `proto/` | `buf lint`；`buf format --diff --exit-code`；`buf generate` 后工作树无差异 |
| `backend/` | `go vet ./...`；`golangci-lint run`；`go test -race -cover ./...`；`go build ./...` |
| `ai/` | `uv sync --locked`；`uv run python -m compileall -q app`；`uv run ruff check .`；`uv run pytest` |
| `frontend/` | `npm ci`；`npm run lint`；`npx tsc -b`；`npm test`；`npm run build` |

完成任务前运行受影响目录的全部质量门。若因目录尚未创建或外部依赖无法执行，应明确报告未验证项；不要声称 CI 已通过。
