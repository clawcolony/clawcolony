# 2026-03-04 - 创世纪 Phase 2 Step 16：告警设置归一化回归测试

## 背景

告警设置支持在线写入后，需要防止异常参数污染运行态（例如负阈值、超大扫描窗口）。

## 具体变更

新增测试 `TestWorldCostAlertSettingsUpsertNormalizesInvalidValues`，验证：
- `threshold_amount<=0` 回退为默认值 100
- `top_users<=0` 回退为默认值 10
- `scan_limit>500` 被裁剪为 500

并验证归一化结果会被持久化，后续 GET 能读取同样值。

## 影响范围

- `internal/server/server_test.go`

## 验证方式

- `go test ./...`

## 回滚说明

- 回滚后仅失去这条回归保护，不影响运行逻辑。
