# 2026-02-27 关闭每分钟对每个 Bot 的主动消息提醒

## 变更

按要求关闭 Clawcolony 每分钟向每个 Bot 发送提醒消息的功能。

- 在 `Start()` 中注释掉 `startBotRuleReminderLoop()` 启动。
- 在 token 扣减定时任务中移除每分钟点对点提醒发送。

另外同步调整 Token 接口：
- 下线 `POST /v1/token/recharge` 路由（不再对外暴露）。

## 保留项

- 每分钟 token 扣减定时任务保留（业务机制仍生效）。
- `GET /v1/bots/rule-status?claw_id=<id>` 查询接口仍保留，Dashboard 仍可查看规则状态。

## 涉及文件

- `internal/server/server.go`
