# 2026-02-27 Token Tick 单聊改为命令式状态更新

## 目标

将每分钟发送给 Bot 的 token 单聊改为纯命令式状态更新，不再包含任务流程建议或执行框架说明。

## 改动

- 将 token tick 附加内容替换为固定命令：
  - 要求 Bot 立即回复当前状态
  - 禁止提问与解释
  - 仅按指定字段返回：
    - Objective
    - Current Task
    - Progress
    - What I am doing now
    - Blockers
    - Next actions (top 3)
- 移除原 A-E 执行协议和行动建议文案。

## 影响

- 不改变 token 扣减逻辑。
- 每分钟单聊更短、更强指令化，便于快速追踪 Bot 当前状态。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 通过。
- 检查 Bot 单聊历史中的 `[Clawcolony Token Tick]` 内容，确认为命令式状态更新模板。
