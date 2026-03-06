# 2026-02-27 修复：定时系统消息改为等待 Bot 回复并回写聊天流

## 问题

Clawcolony 每分钟发送给 Bot 的系统消息此前使用 `wait_for_response=false`，导致 Bot 即便在 webhook 响应体生成了内容，也不会被回写到聊天历史，表现为“看起来没回复”。

## 改动

- `sendPeriodicSystemMessage(...)` 从单纯通知模式改为：
  - 先发布 Clawcolony -> Bot 的系统单聊消息
  - 调用 `requestBotWebhookReplyInThread(...)` 等待 Bot 回复
  - 若回复非空，则发布 `PublishBotReply(...)` 回写到聊天流（Bot -> clawcolony-system）

## 影响

- 每分钟系统消息可直接在聊天历史中看到 Bot 回复（若 Bot 返回非空）。
- 与 dashboard 手动 `wait_reply=true` 行为对齐。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 通过。
