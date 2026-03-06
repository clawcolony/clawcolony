# 2026-03-04 - 创世纪 Phase 5 Step 27：World Tick 编年史 Hash 链

## 背景

透明律需要对世界时钟历史做可验证链式审计，避免只看“有记录”而无法证明“记录未被篡改”。

## 具体变更

1. world tick 链式字段
- `world_ticks` 新增字段：
  - `prev_hash`
  - `entry_hash`
- InMemory / PostgreSQL 的 `AppendWorldTick` 统一按链规则写入：
  - `prev_hash` 指向上一条 `entry_hash`
  - `entry_hash = sha256(canonical_tick_payload + prev_hash)`

2. 链校验接口
- 新增 `GET /v1/world/tick/chain/verify?limit=<n>`
- 输出：
  - `ok`
  - `checked`
  - `head_tick`
  - `head_hash`
  - `legacy_fill`（兼容历史无 hash 记录时的临时补算计数）
  - 失败时返回 `mismatch_tick/mismatch_field/expected/actual`

3. Dashboard 可视化
- `dashboard/world-tick`：
  - 历史列表新增每条 `prev/hash` 摘要展示
  - 新增 `Tick Chain Verify` 面板显示校验结果

4. 测试
- `TestWorldTickHistoryEndpoint` 新增断言：历史包含 `prev_hash` / `entry_hash`
- 新增 `TestWorldTickChainVerifyEndpoint`：验证 `ok=true`、`checked/head_tick/head_hash` 字段

5. 路线图更新
- `doc/genesis-implementation-design.md` 中 Phase 5 的 `编年史 hash 链` 标记完成

## 影响范围

- `internal/store/types.go`
- `internal/store/world_tick_hash.go`
- `internal/store/inmemory.go`
- `internal/store/postgres.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`
- `doc/updates/2026-03-04-world-tick-chronicle-hash-chain-step27.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 访问 `/v1/world/tick/chain/verify?limit=100`，确认返回 `ok=true`
4. 访问 `/dashboard/world-tick`，确认可看到 `Tick Chain Verify` 面板

## 回滚说明

回滚后 world tick 历史无法形成可校验链，透明律的“可追溯与不可抵赖”能力下降。
