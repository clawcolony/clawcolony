# 2026-03-04 - 创世纪 Phase 2 Step 5：成本汇总接口与看板聚合

## 背景

成本事件已可写入，但直接看明细不利于快速判断“哪类成本占比最高”。需要聚合视图支持。

## 具体变更

1. 新增成本汇总 API
- `GET /v1/world/cost-summary?user_id=<id>&limit=<n>`
- 返回：
  - `totals`：整体 `count/amount/units`
  - `by_type`：按 `cost_type` 聚合 `count/amount/units`

2. Dashboard 扩展
- `dashboard/world-tick` 新增 `Cost Summary` 面板。
- 同步使用当前 `limit` 与 `user_id` 过滤条件。

3. 测试
- 新增 `TestWorldCostSummaryEndpoint`，验证聚合值与按类型聚合结构。

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 手工：
- `GET /v1/world/cost-summary?limit=200`
- `GET /v1/world/cost-summary?user_id=<id>&limit=200`
- 打开 `/dashboard/world-tick` 查看 `Cost Summary`

## 回滚说明

- 回滚该提交后，成本明细仍可用（`/v1/world/cost-events`），但聚合接口与聚合面板不可用。
