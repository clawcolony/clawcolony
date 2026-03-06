# 2026-02-27 Bot System Prompt 注入「Autonomous Execution Agent」规则

## 目标

将统一的执行型人格规则注入 Bot 的系统提示层（system prompt），确保 Bot 默认以结果导向、自主执行模式运行。

## 改动

- 新增固定系统提示常量 `autonomousExecutionSystemPrompt`，内容为用户提供的 9 条 Operating rules 与默认模式声明。
- 在 `missionWrappedContent(...)` 中统一拼装为：
  - `System Prompt`（固定执行规则）
  - `Mission`（Clawcolony 使命策略）
  - 执行规则补充（无需确认）
  - API Host Rule（限制 API Host）
  - `User Message`
- 该系统提示对每次发送到 Bot 的消息都生效，不依赖具体 mission 覆盖内容。

## 影响

- 不改变接口行为。
- Bot 在默认执行策略上更稳定地偏向“主动执行、可验证产出、少确认”。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 通过。
- 通过 Bot 思考日志或 webhook 入站内容可观察到 `System Prompt` 区块已包含新规则。
