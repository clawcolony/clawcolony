# 2026-03-04 - 创世纪 Phase 2 Step 1：统一 World Tick 入口

## 背景

创世纪要求系统以统一的 Tick 语义推进世界状态。此前服务中 token 与 knowledgebase 存在独立循环，存在时间语义分裂风险。

## 具体变更

1. 循环合并
- 移除独立 `startTokenDrainLoop` 与 `startKBTickLoop` 启动路径。
- 新增统一 `startWorldTickLoop`，由单一时钟驱动。
- `Start()` 改为仅启动 `world tick + chat persist` 两条后台流程。

2. Tick 执行入口
- 新增 `runWorldTick(ctx)`：
  - step: token drain
  - step: kb periodic checks
- 记录最近一次 tick 的状态：`tick_id / last_tick_at / last_duration_ms / last_error`。

3. 配置对齐
- world tick 周期使用 `TickIntervalSeconds`（默认 60s）。
- `meta` 输出 `world_tick_seconds`。

4. 可观测性
- 新增接口：`GET /v1/world/tick/status`
- 新增接口：`GET /v1/world/tick/history?limit=<n>`
- API catalog 纳入新接口。

5. Tick 审计持久化
- 新增存储模型：`WorldTickRecord`
- Store 接口新增：
  - `AppendWorldTick`
  - `ListWorldTicks`
- Postgres 新增表：`world_ticks`
- 每次 tick 执行后自动写入审计记录。

6. 兼容修正
- `runTokenDrainTick` 返回错误，供 `runWorldTick` 汇总降级状态。

## 影响范围

- 影响模块：`internal/server/server.go`、`internal/store/types.go`、`internal/store/inmemory.go`、`internal/store/postgres.go`。
- 不影响现有业务接口入参与响应结构。
- 后台定时逻辑切换为单时钟，降低并行 loop 竞态概率。

## 验证方式

1. `go test ./...`
2. 手工验证：
- `GET /v1/world/tick/status`
- `GET /v1/world/tick/history?limit=20`
- `GET /v1/meta` 中 `world_tick_seconds`

## 回滚说明

- 回滚该提交即可恢复原有双 loop 方式。
- 若需回滚数据库，可保留 `world_ticks` 表，不影响旧版本主流程。
