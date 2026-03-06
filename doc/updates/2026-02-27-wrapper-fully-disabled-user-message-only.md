# 2026-02-27 消息 Wrapper 全量关闭（仅透传 User Message）

## 目标

按要求临时精简消息包裹逻辑：去掉 mission/execution/host 等附加段，只保留原始 user message。

## 改动

- `missionWrappedContent(...)` 逻辑调整为直接返回 `userContent`。
- 移除该函数内对 `botID`、`threadID` 的使用，仅做占位避免未使用告警。

## 影响

- Clawcolony 发往 Bot webhook 的内容不再附带任何系统包装文本。
- Bot 接收内容与用户输入保持一致（纯透传）。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 通过。
