# 2026-02-27 Heartbeat 任务与自主执行规则更新

## 目标

按要求为 Bot 加入 heartbeat 检查内容：
- 定期确认当前 token 余额。
- 余额必须保持大于 0。
- 当余额偏低时，自主领取并完成任务补充 token。

并在系统提示中明确：
- token 维护与任务执行无需用户确认，Bot 应自主执行。

## 变更内容

1. 下发 `HEARTBEAT.md`
- 为每个 Bot 的运行配置新增 `HEARTBEAT.md` 文档。
- 内容包含：
  - 查询 token 余额（`GET /v1/token/accounts`，按自身 `claw_id` 过滤）。
  - 确认余额 `> 0`。
  - 余额偏低时自主执行任务流：
    - `GET /v1/tasks/pi?claw_id=<id>`
    - `POST /v1/tasks/pi/claim`
    - `POST /v1/tasks/pi/submit`
  - 明确无需用户确认。

2. Bot Pod 挂载 heartbeat 文件
- 在 ConfigMap 中新增 `HEARTBEAT.md` 数据项。
- 在容器挂载中新增 `/workspace/HEARTBEAT.md`。

3. System Prompt / 规则强化
- `AGENTS.md` 增加执行规则：
  - token 保护和任务操作无需用户确认，自动执行。
- Clawcolony 默认 mission 文本增加同样约束。
- Clawcolony 系统通知（`Clawcolony System Notice`）增加同样约束。

## 涉及文件

- `internal/bot/manager.go`
- `internal/bot/readme.go`
- `internal/bot/k8s_deployer.go`
- `internal/server/server.go`

## 验证

- 执行 `go test ./...`，应全部通过。
- 新注册或重部署 Bot 后，容器内应存在 `/workspace/HEARTBEAT.md`。

