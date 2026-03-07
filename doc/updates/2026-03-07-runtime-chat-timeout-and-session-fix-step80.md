# 2026-03-07 Runtime Chat Timeout + Session Fix（Step 80）

## 改了什么

- `internal/server/server.go`
  - `sendChatToOpenClaw`：当内存中没有会话时，不再回退 `--agent main`，改为为每个 user 生成稳定的默认 session id：`runtime-chat-<user_id>`。
  - session-lock 重试改为使用新的 per-user retry session（`runtime-chat-<user_id>-retry-<ts>`），避免退回共享主会话。
  - 将 chat session 持久化统一到 `setChatSession`，确保 JSON 路径与 fallback 路径返回有效 reply 时都能保持会话连续性。
  - `sendChatToOpenClaw` 增加空 `user_id` 保护，提前返回可诊断错误。
  - 新增 `defaultRuntimeChatSessionID(userID string)` 辅助函数。
  - `chatExecTimeoutSeconds`：执行超时上限从 `60s` 调整为 `180s`（仍保持比任务超时短 10s，且最小 20s）。

- `internal/server/server_test.go`
  - 新增 `TestDefaultRuntimeChatSessionID`
  - 新增 `TestNextRuntimeChatRetrySessionID`
  - 新增 `TestCurrentOrDefaultChatSessionID` / `TestCurrentOrDefaultChatSessionIDUsesExisting`
  - 新增 `TestChatExecTimeoutSecondsCap`
  - 新增 `TestSendChatToOpenClawRequiresUserID`

## 为什么改

线上 Dashboard Chat 实测表现为：

- `/v1/chat/send` 能入队并进入 `running`
- 约 60 秒后返回异常碎片（例如 `"propertiesCount": 3`）或“看起来没反应”

根因是两层叠加：

1. 旧逻辑在 session 缺失时使用 `--agent main`，会复用 OpenClaw 主会话，历史上下文非常大，推理耗时显著增加。
2. runtime 对 `openclaw agent` 外层 `timeout` 硬上限为 `60s`，命令在输出完整 JSON 前被截断，导致 fallback 从诊断日志中抽到无意义行。

## 如何验证

- 本地单测：
  - `go test ./internal/server -run 'TestDefaultRuntimeChatSessionID|TestChatExecTimeoutSecondsCap'`
- 回归建议：
  - `go test ./...`
- 线上 smoke：
  - `POST /v1/chat/send`
  - `GET /v1/chat/state?user_id=<id>` 观察 task 不再长时间卡 `running` 且 `recent_statuses.succeeded` 增长
  - `GET /v1/chat/history?user_id=<id>` 检查 reply 为有效文本而非诊断碎片

## 对 agents 的可见变化

- Runtime chat 默认改为“每个 user 独立 session”，不再共用 `main` 会话上下文。
- 在复杂上下文场景下，chat reply 被 60 秒截断的概率显著降低。
