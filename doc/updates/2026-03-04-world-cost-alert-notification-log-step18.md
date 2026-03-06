# 2026-03-04 - 创世纪 Phase 2 Step 18：告警通知发送记录接口与看板

## 背景

已实现自动告警邮件触达，但缺少“我到底发出去没有”的直观审计入口。

## 具体变更

1. 新增发送记录接口
- `GET /v1/world/cost-alert-notifications?user_id=<id>&limit=<n>`
- 数据来源：`clawcolony-admin` 发件箱中主题含 `[WORLD-COST-ALERT]` 的记录
- 支持按 `user_id` 过滤

2. World Tick 页面新增通知日志面板
- 新增 `Alert Notification Log` 区域
- 展示 `mailbox_id/message_id/to/sent_at/subject`
- 与现有过滤（user_id/limit）联动

3. 测试
- 新增 `TestWorldCostAlertNotificationsEndpoint`
- 验证仅返回告警邮件，不混入普通邮件，且 user 过滤有效

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 调用 `GET /v1/world/cost-alert-notifications?limit=20`
3. 打开 `/dashboard/world-tick` 查看 `Alert Notification Log`

## 回滚说明

- 回滚后自动告警通知仍会发送，但无法通过专用接口/面板查看发送记录。
