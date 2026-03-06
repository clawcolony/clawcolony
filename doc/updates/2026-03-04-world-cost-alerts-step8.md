# 2026-03-04 - 创世纪 Phase 2 Step 8：高消耗告警（仅观测）

## 背景

已有成本明细与聚合，但缺少“按用户”的风险视角，无法快速识别近期高消耗用户。

## 具体变更

1. 新增告警接口
- `GET /v1/world/cost-alerts?user_id=<id>&threshold_amount=<n>&limit=<n>&top_users=<n>`
- 逻辑：基于最近 `limit` 条成本事件，按用户聚合 amount/units/event_count，筛选 `amount >= threshold_amount`。
- 输出每个告警用户的 `top_cost_type/top_cost_amount`。
- 该接口仅用于观测，不触发任何拦截动作。

2. Dashboard 扩展
- `dashboard/world-tick` 新增 `High Cost Alerts (Observation Only)` 面板。
- 支持阈值输入（`alert threshold`）并实时刷新。

3. 测试
- 新增 `TestWorldCostAlertsEndpoint`，验证阈值筛选与 top_cost_type 计算。

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 手工：
- `GET /v1/world/cost-alerts?threshold_amount=100&limit=500`
- 打开 `/dashboard/world-tick`，调整 `alert threshold`，确认告警列表变化

## 回滚说明

- 回滚后不再提供告警接口和告警面板；成本明细与汇总不受影响。
