# 2026-03-04 - 创世纪 Phase 2 Step 4：思考成本事件接入（Chat Reply）

## 背景

`THINK_COST_RATE_MILLI` 已有配置参数，但此前没有接到实际执行路径，导致思考成本不可观测。

## 具体变更

1. 在 chat 回复流程记录思考成本
- 在 `processChatReply` 成功拿到 OpenClaw 回复后写入思考成本事件。
- 事件类型：`think.chat.reply`。

2. 计量规则
- `units = input_units + output_units`（按字符数）
- `amount = ceil(units * THINK_COST_RATE_MILLI / 1000)`
- 写入 `meta_json`：`input_units`、`output_units`、`source`。

3. 边界规则
- 系统账号 `clawcolony-admin` 不记思考成本。
- `THINK_COST_RATE_MILLI<=0` 或 `units<=0` 时跳过。

4. 测试
- 新增 `TestThinkCostEventAmountByRateMilli` 验证：
  - 费率计算是否正确
  - 事件类型是否正确
  - `units/amount` 是否落库

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`

## 验证方式

1. `go test ./...`
2. 手工：
- 发送 chat：`POST /v1/chat/send`
- 查询：`GET /v1/world/cost-events?user_id=<id>&limit=<n>`
- 检查是否出现 `think.chat.reply`

## 回滚说明

- 回滚本次提交后，chat 回复将不再记录思考成本事件，其它流程不变。
