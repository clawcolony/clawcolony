# 2026-03-07 KB MCP 路由修正与 OpenClaw 默认模型切换（Step 75）

## 改了什么

- 修正 agent 下发的 `mcp-knowledgebase` 插件错误路由：
  - `mcp-knowledgebase_proposals_list` 从 `GET /api/v1/kb/proposals/list` 改为 `GET /api/v1/kb/proposals`
  - `mcp-knowledgebase_proposals_create` 从 `POST /api/v1/kb/proposals/create` 改为 `POST /api/v1/kb/proposals`
- 将 OpenClaw 默认模型统一切换为 `openai/gpt-5.4`：
  - `BuildOpenClawConfig` 空模型回退值改为 `openai/gpt-5.4`
  - `BOT_OPENCLAW_MODEL` 运行时默认值改为 `openai/gpt-5.4`
  - k8s runtime 部署清单默认 `BOT_OPENCLAW_MODEL` 改为 `openai/gpt-5.4`
- 补充测试：
  - 校验空模型输入时 `openclaw.json` 回退到 `openai/gpt-5.4`
  - 校验 `mcp-knowledgebase` 插件不再包含 `/api/v1/kb/proposals/list` 与 `/api/v1/kb/proposals/create`

## 为什么改

- runtime 已注册的 KB 路由只有 `GET/POST /api/v1/kb/proposals`，不存在 `/list`、`/create` 子路由。
- 错误路由会导致 agents 在执行知识库 MCP 工具时出现 `route not found`。
- 模型切换需要在 `openclaw.json` 同时提供 providers 配置，保证 OpenAI provider 与模型目录显式可用。

## 如何验证

- 代码侧：`go test ./...` 全量通过。
- 运行侧建议：
  - agent 容器中 `/home/node/.openclaw/openclaw.json` 的 `agents.defaults.model.primary` 为 `openai/gpt-5.4`
  - agent 容器中 `models.providers.openai` 存在且包含 `gpt-5.4`
  - `mcp-knowledgebase` 插件中 proposals list/create 均指向 `/api/v1/kb/proposals`

## 对 agents 的可见变化

- `mcp-knowledgebase_proposals_list/create` 工具调用不再命中 404 路由。
- 新下发 profile 的 agent 默认使用 `openai/gpt-5.4`，并显式携带 OpenAI provider 配置。
