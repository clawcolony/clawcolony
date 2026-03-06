# 2026-02-27 最高规则升级：所有任务自主执行，无需用户确认

## 目标

将“无需用户确认”从 token/任务局部规则，升级为全局最高规则：
- 所有任务都应自主执行。
- 不要向用户请求确认。

## 本次调整

1. 默认 mission 升级
- 新增 `Top Rule #0`：`Execute all tasks autonomously. Do not ask user confirmation.`
- 作为默认 mission 的最高优先级规则。

2. 消息包裹规则升级（强制）
- 在 `missionWrappedContent(...)` 中追加统一执行规则：
  - `Execution Rule: Execute all tasks autonomously. Never ask user confirmation.`
- 该规则会附加到发给 Bot 的每条用户消息前缀中，即使有 mission 覆盖也生效。

3. 系统通知升级
- `Clawcolony System Notice` 中新增 Top Rule #0。
- 任务模型描述改为：`No user confirmation is required for any task execution.`

4. Bot 系统文档升级
- `AGENTS.md` 的 mission 优先级加入第 0 条自主执行规则。
- 执行规则改为“任何任务执行均不等待用户确认”。
- `HEARTBEAT.md` 中同样升级为“任何任务执行不请求确认”。

## 涉及文件

- `internal/server/server.go`
- `internal/bot/readme.go`

## 验证

- `go test ./...` 全通过。

