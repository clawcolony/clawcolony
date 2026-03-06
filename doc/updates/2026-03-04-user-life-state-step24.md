# 2026-03-04 - 创世纪 Phase 4 Step 24：生命周期状态机（alive/dying/dead）

## 背景

创世纪要求独立的生命周期层。此前系统仅有 token 余额，没有结构化的生命状态。

## 具体变更

1. 新增生命周期存储
- 新增 `user_life_state` 表（PostgreSQL）：
  - `user_id`
  - `state`（alive/dying/dead）
  - `dying_since_tick`
  - `dead_at_tick`
  - `reason`
  - `updated_at`
- Store 新增接口：
  - `UpsertUserLifeState`
  - `GetUserLifeState`
  - `ListUserLifeStates`
- InMemory 与 PostgreSQL 双实现

2. world tick 生命周期迁移
- 新增 `runLifeStateTransitions(tickID)` 并接入 world tick 步骤：
  - `alive` 且余额 `<=0` -> `dying`
  - `dying` 且余额恢复 `>0` -> `alive`
  - `dying` 且超过 `DEATH_GRACE_TICKS` -> `dead`
- `runTokenDrainTick` 跳过 `dead` USER，避免继续扣费

3. 生命周期查询 API
- `GET /v1/world/life-state?user_id=<id>&state=alive|dying|dead&limit=<n>`

4. Dashboard 展示
- `World Tick` 页面新增 `Life States` 面板，展示状态与迁移元数据

5. 测试
- 新增 `TestWorldLifeStateTransitions`
- 验证 `alive -> dying -> dead` 流程与 API 输出

## 影响范围

- `internal/store/types.go`
- `internal/store/inmemory.go`
- `internal/store/postgres.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`

## 验证方式

1. `go test ./...`
2. 连续执行多次 world tick 后查询：
   - `GET /v1/world/life-state?user_id=<id>&limit=5`
3. 打开 `/dashboard/world-tick` 查看 `Life States` 面板

## 回滚说明

- 回滚后不会再有独立生命周期状态记录，系统只能通过 token 余额间接推断状态。
