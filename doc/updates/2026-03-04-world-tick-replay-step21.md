# 2026-03-04 - 创世纪 Phase 2 Step 21：World Tick 重放能力

## 背景

完成步骤审计后，还需要一个“可触发重放”的入口，便于在开发阶段快速复现 world tick 处理链路并观察行为。

## 具体变更

1. world_ticks 审计增强
- 新增字段：
  - `trigger_type`（`scheduled` / `replay`）
  - `replay_of_tick_id`
- PostgreSQL 自动迁移补齐历史库表字段（`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`）

2. 新增 replay API
- `POST /v1/world/tick/replay`
- 入参：`source_tick_id`（可选，不填时默认取当前最近 tick）
- 返回：`source_tick_id`、`replay_tick_id`

3. 执行链路改造
- `runWorldTickWithTrigger(...)` 统一执行 scheduled/replay
- replay 通过 `runWorldTickReplay(...)` 触发并落库为 `trigger_type=replay`

4. Dashboard 支持 replay
- `World Tick` 页面新增：
  - `replay source tick_id` 输入框
  - `重放 Tick` 按钮
  - replay 执行状态提示
- Tick 历史行新增显示：`trigger` 与 `replay_of`

5. 测试
- 新增 `TestWorldTickReplayEndpoint`
- 验证 replay 调用可生成新 tick，并在历史中带 replay 审计标记

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
2. 调用 `POST /v1/world/tick/replay`（携带 `source_tick_id`）
3. 调用 `GET /v1/world/tick/history?limit=<n>`，确认出现 `trigger_type=replay`
4. 打开 `/dashboard/world-tick` 使用 replay 按钮并观察历史变化

## 回滚说明

- 回滚后将失去 replay 入口与 replay 审计字段展示；scheduled tick 不受影响。
