# 2026-03-04 - 创世纪 Phase 5 Step 29：World Replay Dashboard 回放页

## 背景

Phase 5 需要将编年史链结果可视化，并支持按 tick 回看“发生了什么”。单纯 API 不够，需要专门回放页给运维与调试使用。

## 具体变更

1. 新增回放页面
- 新页面：`/dashboard/world-replay`
- 功能：
  - 左侧时间线：最近 ticks（含 prev/hash 摘要）
  - 右侧快照：选中 tick 的完整信息
  - 右侧步骤：`/v1/world/tick/steps?tick_id=<id>`
  - 右侧成本：`/v1/world/cost-events?tick_id=<id>`
  - 右侧链校验：`/v1/world/tick/chain/verify`

2. Dashboard 路由与入口
- `internal/server/dashboard.go` 新增路由映射：
  - `dashboard/world-replay -> dashboard_world_replay.html`
- 首页新增 `World Replay` 卡片入口
- `dashboard/world-tick` 顶部 tab 新增 `World Replay` 跳转

3. 成本事件按 tick 过滤
- 扩展 `GET /v1/world/cost-events`：
  - 新增可选参数 `tick_id`
  - 当传入 `tick_id` 时仅返回该 tick 的事件

4. 测试
- 扩展 `TestWorldCostEventsEndpoint`：
  - 覆盖 `tick_id` 过滤有效性
  - 断言不会混入其他 tick 的事件

5. 路线图更新
- `doc/genesis-implementation-design.md` Phase 5 `Dashboard 回放页` 标记完成

## 影响范围

- `internal/server/dashboard.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_home.html`
- `internal/server/web/dashboard_world_tick.html`
- `internal/server/web/dashboard_world_replay.html`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`
- `doc/updates/2026-03-04-world-replay-dashboard-step29.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 打开 `/dashboard/world-replay`：
  - 选择 tick 后可看到 steps/costs 快照
  - `chain verify` 面板正常展示

## 回滚说明

回滚后需手工拼多个 API 才能回放一个 tick，排障效率下降。
