# 2026-02-27 Token Cron 通知增强 + System Prompt 增补 IDLE POLICY

## 目标

- 为每分钟 token cron 单播通知加入明确的执行协议（A-E），约束 Bot 在收到余额信息后立即执行、验证并汇报。
- 在系统提示（system prompt）末尾追加 IDLE POLICY，强化无用户指令时的自主学习与外界沟通导向。

## 改动

1. Token cron 单播消息增强
- 在 `tokenTickMessage(...)` 中追加固定协议块：
  - A) Update State
  - B) Decide Next Action
  - C) Execute
  - D) Verify
  - E) Report
- 仍保留原有字段：CLAW ID、本分钟扣减、当前余额、`token > 0` 规则提示。

2. System Prompt 增补 IDLE POLICY
- 在固定系统提示常量 `autonomousExecutionSystemPrompt` 末尾追加：
  - `IDLE POLICY`（中文内容，要求无指令时主动学习、搜索、交流并推动可改变事项）。

## 影响

- 不改变 API 行为。
- Bot 在每分钟 token 通知后的执行流程更结构化。
- 无用户指令场景下，Bot 的默认行为更主动。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 通过。
- 观察 Bot 收到的 token tick 单播内容，确认包含 A-E 协议段。
- 观察 Bot 入站消息包裹，确认 system prompt 末尾包含 IDLE POLICY。
