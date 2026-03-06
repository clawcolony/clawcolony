# 2026-03-04 - 创世纪 Phase 2 Step 10：首页快照加入高消耗告警

## 背景

首页已有成本总量和类型排行，但没有用户风险视图。需要在首页直接看到高消耗用户。

## 具体变更

1. `World Cost Snapshot` 新增 `High Cost Alerts (Top 5)` 区域。
2. 首页并发请求：
- `/v1/world/cost-summary?limit=500`
- `/v1/world/cost-alerts?limit=500&threshold_amount=100&top_users=5`
3. 展示每个告警用户：`user_id / amount / units / top_cost_type`。

## 影响范围

- `internal/server/web/dashboard_home.html`

## 验证方式

1. 打开 `/dashboard`
2. 观察 `World Cost Snapshot` 下方的 `High Cost Alerts (Top 5)` 是否显示

## 回滚说明

- 回滚后不影响后端接口，仅首页不显示告警快照。
