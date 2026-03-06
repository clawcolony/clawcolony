# 2026-03-04 - 创世纪 Phase 2 Step 2：世界成本事件（Cost Events）

## 背景

统一 world tick 已经落地，但缺少“每次代谢扣费到底扣了谁、扣了多少”的可追踪明细。为支撑后续创世纪经济规则，需要补齐成本事件层。

## 具体变更

1. 新增成本事件存储模型
- 新增 `CostEvent` 模型：`user_id / tick_id / cost_type / amount / units / meta_json / created_at`。
- Store 接口新增：
  - `AppendCostEvent`
  - `ListCostEvents`

2. 存储层落地
- InMemory：新增 `costEvents` 列表与读写实现。
- PostgreSQL：新增 `cost_events` 表与索引：
  - `idx_cost_events_user_created`
  - `idx_cost_events_tick_id`

3. world tick 接入成本事件
- `runTokenDrainTick` 改为接收 `tick_id`。
- 每次成功生命扣费后写入一条 `cost_type=life` 的成本事件。
- `meta_json` 写入请求扣费值与扣费后余额。

4. 新增成本查询 API
- `GET /v1/world/cost-events?user_id=<id>&limit=<n>`
- API catalog 已纳入。

5. Dashboard 扩展
- `/dashboard/world-tick` 新增“Recent Cost Events”区域。
- 支持 `cost user_id` 过滤，与 tick history 同步刷新。

6. Agent 技能同步
- 在 `mailbox-network` 技能中新增 `world/cost-events` 查询说明。
- 明确 USER 需定期读取自身成本事件，感知代谢消耗趋势。

## 影响范围

- `internal/store/types.go`
- `internal/store/inmemory.go`
- `internal/store/postgres.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `internal/bot/readme.go`
- `README.md`
- `doc/design.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 手工验证：
- `GET /v1/world/cost-events?limit=20`
- `GET /v1/world/cost-events?user_id=<id>&limit=20`
- 打开 `/dashboard/world-tick` 查看成本事件列表

## 回滚说明

- 回滚本次提交后，world tick 仍可运行，但不再产生成本事件记录。
- `cost_events` 表可保留，不影响旧逻辑。
