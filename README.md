# SpeakUp

SpeakUp 是一个 Web 端 AI 英语口语陪练 MVP。浏览器只连接 Go 网关；网关通过 gRPC 调用本机 Python AI 服务，并通过 Kafka 驱动异步反馈。

## 本地启动

要求：Docker Compose、Go 1.25、Node.js 22、uv、Buf。

1. 准备配置：

   ```bash
   cp docs/.env.example .env
   ```

   默认 `AI_PROVIDER=fake`，不需要供应商密钥。要启用通义文本回复，将其改为 `dashscope` 并填写 `DASHSCOPE_API_KEY`。

2. 启动基础设施：

   ```bash
   docker compose -f docker/compose.yml up -d mysql redis kafka
   docker compose -f docker/compose.yml up kafka-init
   ```

   为避免与本机其他项目冲突，默认端口为 MySQL `13306`、Redis `16379`、Kafka `19092`。

3. 分别在三个终端运行应用：

   ```bash
   cd ai && uv sync --locked && uv run python -m app.main
   cd backend && go run ./cmd/gateway
   cd frontend && npm ci && npm run dev
   ```

4. 浏览器打开 `http://localhost:5173`，可直接注册。开发示例密码需至少 8 位。

## 验证

各目录质量门与 CI 一致：

```bash
cd proto && buf lint && buf format --diff --exit-code
cd backend && go vet ./... && golangci-lint run && go test -race -cover ./... && go build ./...
cd ai && uv sync --locked && uv run python -m compileall -q app && uv run ruff check . && uv run pytest
cd frontend && npm ci && npm run lint && npx tsc -b && npm test && npm run build
```

三端和基础设施运行后，可执行 `bash scripts/smoke.sh` 验证注册、场景、文字 WebSocket、回合持久化、Kafka 反馈和评估查询。

## 停止

先在应用终端按 Ctrl+C，再停止基础设施：

```bash
docker compose -f docker/compose.yml down
```

默认保留具名 volume。只有明确需要清空本地开发数据时才使用 `down -v`。
