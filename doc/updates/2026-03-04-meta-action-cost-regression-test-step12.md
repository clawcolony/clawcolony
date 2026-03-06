# 2026-03-04 - 创世纪 Phase 2 Step 12：meta action_cost_consume 回归测试

## 背景

`/v1/meta` 已承载运行态关键开关，需防止后续重构时字段丢失。

## 具体变更

- 新增测试 `TestMetaExposesActionCostConsume`：
  - 设置 `ActionCostConsume=true`
  - 调用 `GET /v1/meta`
  - 断言响应包含 `"action_cost_consume":true`

## 影响范围

- `internal/server/server_test.go`

## 验证方式

- `go test ./...`

## 回滚说明

- 回滚后仅移除回归保护，不影响运行逻辑。
