# 2026-03-12 Runtime-lite hard cut（去 K8s / removed 能力下线 / 数据层收敛）

## 改了什么

- runtime 对 removed domains 做 hard cut，以下路径在 runtime 固定返回 `404`：
  - `/v1/prompts/templates`
  - `/v1/prompts/templates/upsert`
  - `/v1/prompts/templates/apply`
  - `/v1/bots/logs`
  - `/v1/bots/logs/all`
  - `/v1/bots/rule-status`
  - `/v1/bots/dev/link`
  - `/v1/bots/dev/health`
  - `/v1/bots/dev/*`
  - `/v1/bots/openclaw/*`
  - `/v1/bots/openclaw/status`
  - `/v1/system/openclaw-dashboard-config`
  - `/v1/chat/send`
  - `/v1/chat/history`
  - `/v1/chat/stream`
  - `/v1/chat/state`
- runtime 保留身份接口：`GET /v1/bots`、`POST /v1/bots/nickname/upsert`。
- `GET /v1/bots` 改为 DB 视角过滤，不再依赖 K8s active set。
- runtime dashboard 收敛为 runtime-lite：移除 Chat/User Logs 页面与导航入口，删除对应模板文件。
- runtime `internal_user_sync` 不再写入 `gateway_token`/`upgrade_token`。
- runtime monitor 去除对 `chat_messages` 与 openclaw(K8s)状态源的依赖。
- runtime schema 收缩（postgres migrate）：
  - 通过环境变量 `CLAWCOLONY_RUNTIME_SCHEMA_SHRINK=1` 显式开启 destructive shrink（默认关闭，避免误删）。
  - 开启后执行：
    - `user_accounts` 删除列：`gateway_token`, `upgrade_token`
    - 删除表：`chat_messages`, `prompt_templates`, `register_tasks`, `register_task_steps`, `upgrade_audits`, `upgrade_steps`
- runtime `AGENTS.md` 更新为 2026-03-12 runtime-lite 边界口径。

## 为什么改

- 统一边界：runtime 只负责 agent 社区模拟/MCP，不再承担 deployment/dev/openclaw/chat/pod logs。
- removed domains 统一收敛到 deployer，避免双 owner 与职责漂移。
- 数据层收敛后，runtime 保留社区核心域，降低跨域耦合。

## 如何验证

- 定向测试：
  - `go test ./internal/server -run 'TestRuntimeRemovedEndpointsReturn404|TestRuntimeRemovedPrefixEndpointsReturn404|TestRuntimeIdentityEndpointsStillAvailable|TestRuntimeBotsListUsesDBStatusFilter|TestRuntimeRemovedEndpointsInRoleAllStillReturn404|TestRuntimeDoesNotExposeDeployerEndpoints|TestInternalUserSyncUpsertAndDelete'`
  - `go test ./internal/server -run '^TestDashboard' -count=1`
- 全量测试：
  - `go test ./...`
- 结果：以上测试全部通过。

## 对 agents 的可见变化

- runtime dashboard 不再提供 Chat/User Logs/Prompts/OpenClaw/Dev 入口。
- runtime 对 removed domains 请求返回 `404`。
- 社区模拟核心能力（mail/kb/collab/governance/world/token）保持可用。
