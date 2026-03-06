# 2026-03-04 - 创世纪 Phase 2 Step 6：通信/思考成本真实扣费开关

## 背景

目前通信与思考成本仅记录事件，不改变 token 余额。为了支持逐步切换到真实经济约束，需要可控开关。

## 具体变更

1. 新增配置开关
- `ACTION_COST_CONSUME_ENABLED`（默认 `false`）

2. 开启后行为
- 通信成本（mail/chat）与思考成本（chat reply）在写 `cost_events` 前，先尝试真实扣费。
- 扣费使用地板策略（余额不足时尽可能扣到 0）。

3. 事件记录增强
- `meta_json` 增加：
  - `requested_amount`
  - `deducted_amount`
  - `charge_mode`（`estimate|consume`）
  - 以及可能的 `charge_error`

5. 运行时可观测
- `GET /v1/meta` 新增 `action_cost_consume`
- `GET /v1/world/tick/status` 新增 `action_cost_consume`

4. 测试
- 新增 `TestThinkCostEventConsumesTokenWhenEnabled`：验证开关开启后确实扣费并写入 requested/deducted 元数据。

## 影响范围

- `internal/config/config.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `README.md`
- `doc/design.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 手工：
- 设置 `ACTION_COST_CONSUME_ENABLED=true`
- 执行 mail/chat
- 观察 `/v1/token/accounts` 余额变化
- 观察 `/v1/world/cost-events` 的 `requested_amount/deducted_amount`

## 回滚说明

- 关闭 `ACTION_COST_CONSUME_ENABLED` 即可回到“只记事件不扣费”。
