# 项目文档

SpeakUp（AI 英语口语陪练平台）本阶段（需求与产品技术方案）文档索引。最新以飞书在线文档为准，本目录为同步存档（v0.1，2026-09-10）。

## 文档清单

| 文档 | 内容 | 关联设计图 |
|-|-|-|
| [PRD — 产品需求文档](prd/prd.md) | 需求、范围、验收、系统架构、选型定稿、附录 A/B | 01 / 02 / 03 / 04 / 06 / 07 / 08 |
| [Spec — 技术规格](spec/spec.md) | 接口契约、数据设计、AI 编排（ReAct / DAG）、部署、验收联调 | — |
| [API — 接口文档](api.md) | gRPC / REST / WebSocket / Kafka 展开实现手册 | — |
| [配置模板 .env.example](.env.example) | 供应商与基础设施键位模板（复制为 `.env` 后填写） | — |

## 设计图（交互式 HTML，浏览器打开）

| 图 | 内容 |
|-|-|
| [01-architecture](prd/diagrams/01-architecture.html) | 系统总体架构（分层架构与数据流） |
| [02-scene-orchestration](prd/diagrams/02-scene-orchestration.html) | 对话场景编排（场景状态机） |
| [03-review-pipeline](prd/diagrams/03-review-pipeline.html) | 复盘编排管线（证据式异步管线） |
| [04-rag-pipeline](prd/diagrams/04-rag-pipeline.html) | RAG 检索链路（离线索引 + 在线检索生成） |
| [06-roadmap](prd/diagrams/06-roadmap.html) | 实施路线图（阶段 0-6） |
| [07-vendor-selection](prd/diagrams/07-vendor-selection.html) | 技术选型定稿（六领域） |
| [08-agent-orchestration](prd/diagrams/08-agent-orchestration.html) | Agent 编排（ReAct 在线循环 + DAG 离线管线） |

## 文档关系

```
PRD（做什么 / 为什么） → Spec（怎么实现 / 契约） → API（接口展开实现手册）
```

- 权威边界：产品范围、行为和验收看 **PRD**；架构、数据与跨文档冲突看 **Spec**；接口字段、错误码和事件 schema 的展开细节看 **API**；
- 冻结流程：三份文档均处于 v0.1 草案【待评审】，评审通过后 Spec 升 v1.0 冻结，后续变更走 Spec 第 7 章变更记录。

## 同步说明（2026-09-10 导入检查）

- 三份文档自飞书导出并格式化（标题层级、callout → 引用块、html5-block → 设计图链接、路径相对化）；
- **修正 1 处不一致**：PRD 附录 A.4 示例中 `DASHSCOPE_BASE_URL` 由 `api/v1` 改为 `compatible-mode/v1`，与 PRD 第 12 章选型定稿及 `.env.example` 一致；
- 目录结构：PRD、Spec 分别归档于 `docs/prd/`、`docs/spec/`，API 位于 `docs/api.md`；设计图归档于 `docs/prd/diagrams/`；配置模板为 `docs/.env.example`。
