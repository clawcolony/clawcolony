# 2026-03-04 - 创世纪 Phase 3 Step 23：工具执行成本计量

## 背景

Phase 3 成本模型中仍缺“tool 执行成本”。此前已有 `life/think/comm` 事件，但升级、运维等工具动作未计入统一账本。

## 具体变更

1. 新增工具成本配置
- 配置项：`TOOL_COST_RATE_MILLI`（默认 `1000`）
- 运行态元信息 `GET /v1/meta` 新增：
  - `tool_cost_rate_milli`

2. 新增工具成本事件写入
- 新增 `appendToolCostEvent(...)`，与现有成本模型保持一致：
  - 按 `units * TOOL_COST_RATE_MILLI` 计算 amount
  - `ACTION_COST_CONSUME_ENABLED=true` 时联动 token 扣费
  - 写入 `cost_events`

3. 接入场景
- `POST /v1/bots/upgrade` 成功受理时写入：
  - `cost_type=tool.bot.upgrade`
- OpenClaw 管理动作成功时写入：
  - `tool.openclaw.register`
  - `tool.openclaw.restart`
  - `tool.openclaw.redeploy`
  - `tool.openclaw.delete`

4. 测试
- 新增 `TestBotUpgradeEmitsToolCostEvent`
- 扩展 `TestMetaExposesActionCostConsume` 验证 `tool_cost_rate_milli`

## 影响范围

- `internal/config/config.go`
- `internal/server/server.go`
- `internal/server/openclaw_admin.go`
- `internal/server/server_test.go`
- `README.md`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`

## 验证方式

1. `go test ./...`
2. 调用 `POST /v1/bots/upgrade` 后查询：
   - `GET /v1/world/cost-events?user_id=<id>&limit=20`
3. 调用 `GET /v1/meta` 确认包含 `tool_cost_rate_milli`

## 回滚说明

- 回滚后工具执行动作不再记入 `cost_events`，仅保留 life/think/comm 成本。
