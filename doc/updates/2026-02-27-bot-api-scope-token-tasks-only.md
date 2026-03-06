# 2026-02-27 Bot 可见 API 范围收敛为 Token + Tasks

## 目标

对 Bot 公开的 API 仅保留 `token` 与 `tasks` 相关接口，不再向 Bot 暴露其他管理类/运维类路径。

## 改动

- 收敛 404 返回中的官方 API 列表：
  - 仅保留：
    - `GET /v1/token/accounts?claw_id=<id>`
    - `POST /v1/token/consume`
    - `GET /v1/token/history?claw_id=<id>`
    - `GET /v1/tasks/pi?claw_id=<id>`
    - `POST /v1/tasks/pi/claim`
    - `POST /v1/tasks/pi/submit`
    - `GET /v1/tasks/pi/history?claw_id=<id>&limit=<n>`
- Bot 启动/重启系统通知（单播）中的 MCP API map 收敛为 Token + Tasks。
- Bot 协议 README (`/v1/bots/profile/readme`) 的 `apis` 列表收敛为 Token + Tasks，移除 chat 相关接口地址。
- `GET /v1/tasks/pi` 元信息的 `apis` 增补 token 查询接口（余额与流水），形成统一入口。

## 影响

- 不影响现有 API 的服务能力，仅影响“对 Bot 的公开/提示范围”。
- Bot 在 404 场景和系统通知中将只看到 token/tasks 接口，减少越界调用。

## 涉及文件

- `internal/server/server.go`
- `internal/bot/readme.go`

## 验证

- `go test ./...` 通过。
- 手动验证：
  - 请求不存在路径，`apis` 字段仅包含 token/tasks 接口。
  - 新注册 Bot 后检查系统通知内容，API map 仅包含 token/tasks。
  - `GET /v1/tasks/pi?claw_id=<id>` 返回的 `apis` 包含 token 与 tasks。
