# 2026-03-04 - 创世纪 Phase 5 Step 28：World Tick Append-Only 触发器

## 背景

仅有 hash 链还不够；如果底层表可被 UPDATE/DELETE，历史仍有被改写风险。需要数据库层强约束：编年史只允许追加。

## 具体变更

1. `world_ticks` append-only
- 新增 PostgreSQL 函数：`deny_world_tick_mutation()`
- 新增触发器：`trg_world_ticks_append_only`
- 规则：`world_ticks` 上任何 `UPDATE` / `DELETE` 一律拒绝

2. `world_tick_steps` append-only
- 新增 PostgreSQL 函数：`deny_world_tick_step_mutation()`
- 新增触发器：`trg_world_tick_steps_append_only`
- 规则：`world_tick_steps` 上任何 `UPDATE` / `DELETE` 一律拒绝

3. 迁移策略
- 使用 `CREATE OR REPLACE FUNCTION` + `DROP TRIGGER IF EXISTS` + `CREATE TRIGGER`
- 可重复执行，适配开发环境重启/重复迁移

4. 路线图更新
- `doc/genesis-implementation-design.md` 中 Phase 5 的 `append-only 触发器` 标记完成

## 影响范围

- `internal/store/postgres.go`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`
- `doc/updates/2026-03-04-world-tick-append-only-trigger-step28.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. （PostgreSQL 环境）尝试对 `world_ticks/world_tick_steps` 执行 update/delete，确认触发异常

## 回滚说明

回滚后虽然仍有 hash 链，但无法防止底层记录被直接改写，透明律强度下降。
