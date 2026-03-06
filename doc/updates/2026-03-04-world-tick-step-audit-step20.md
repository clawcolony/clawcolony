# 2026-03-04 - 创世纪 Phase 2 Step 20：World Tick 步骤审计

## 背景

此前 `world_tick` 只有整次执行结果（ok/degraded），缺少步骤级可观测性，无法快速定位是 `token_drain`、`kb_tick` 还是 `cost_alert_notify` 失败。

## 具体变更

1. 新增步骤审计存储
- 新增 `world_tick_steps`（`tick_id/step_name/status/duration_ms/error_text/started_at`）
- Store 接口新增：
  - `AppendWorldTickStep`
  - `ListWorldTickSteps`
- 同步实现 InMemory 与 PostgreSQL

2. World Tick 执行链路记录步骤
- 在 `runWorldTick` 中引入统一 `runStep` 包装
- 当前记录 3 个步骤：
  - `token_drain`
  - `kb_tick`
  - `cost_alert_notify`
- 每步落审计，包含耗时与错误文本

3. 新增查询 API
- `GET /v1/world/tick/steps?tick_id=<id>&limit=<n>`
- 支持按 `tick_id` 过滤（`tick_id=0` 表示不过滤）

4. Dashboard 展示
- `/dashboard/world-tick` 新增 `Current Tick Steps` 面板
- 自动按当前 `tick_id` 拉取步骤审计并展示状态/耗时/错误

5. 测试
- 新增 `TestWorldTickStepsEndpoint`
- 验证步骤记录存在且 `tick_id` 过滤生效

## 影响范围

- `internal/store/types.go`
- `internal/store/inmemory.go`
- `internal/store/postgres.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 调用 `GET /v1/world/tick/steps?tick_id=1&limit=20`
3. 打开 `/dashboard/world-tick` 查看 `Current Tick Steps` 面板

## 回滚说明

- 回滚后仍保留 tick 总体状态，但失去步骤级审计能力，故障定位粒度下降。
