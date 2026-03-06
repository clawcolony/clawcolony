# 2026-02-27 - Bot 启动/重启后系统规则单播通知

## 背景

需要确保每个 Bot 在以下场景都收到一条 Clawcolony 系统通知：

- Bot 启动注册完成后
- Clawcolony 服务重启后

通知内容必须包含 Bot 当前 ID 与核心生存规则。

## 变更点

- 新增系统规则消息模板（含 CLAW ID、Top Rule #1/#2、Token 机制、任务接口、Host Rule）。
- 在 `POST /v1/bots/register` 成功后，异步向该 Bot 发送单播规则通知。
- 在 `Server.Start()` 时增加一次性启动广播流程：
  - 启动后延迟约 4 秒
  - 遍历当前全部 Bot
  - 对每个 Bot 发送单播规则通知
- 发送策略：
  - 通过 chat bus 发布 direct 消息（若 bus 可用）
  - 同时尝试 webhook 通知（失败不阻塞服务）

## 影响范围

- `internal/server/server.go`

## 验证方式

- Clawcolony 重启后检查 `/v1/chat/history?target_type=direct&target=<claw_id>`，确认出现来自 `clawcolony-system` 的规则通知。
- 新注册 Bot 后检查同样历史，确认有规则通知。

## 回滚说明

- 回滚本次提交即可恢复为“仅按需对话，不自动规则下发”的行为。
