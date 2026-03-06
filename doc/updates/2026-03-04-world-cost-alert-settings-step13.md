# 2026-03-04 - 创世纪 Phase 2 Step 13：高消耗告警配置持久化

## 背景

高消耗告警接口已可用，但阈值/top_users/扫描窗口需要稳定配置，不应每次都依赖 query 参数。

## 具体变更

1. 新增持久化存储
- 新增 `world_settings`（key/value/updated_at）
- store 接口新增：
  - `GetWorldSetting`
  - `UpsertWorldSetting`

2. 新增告警配置接口
- `GET /v1/world/cost-alert-settings`
- `POST /v1/world/cost-alert-settings/upsert`

3. 告警接口默认读取配置
- `GET /v1/world/cost-alerts` 在 query 参数缺省时使用持久化设置。

4. Dashboard 接入
- world tick 页新增可编辑输入：
  - `alert threshold`
  - `alert top_users`
  - `alert scan_limit`
- 新增“保存告警设置”按钮，保存后立即生效。

5. 测试
- `TestWorldCostAlertSettingsEndpoints`
- `TestWorldCostAlertsUsesStoredSettingsByDefault`

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
2. 手工：
- 打开 `/dashboard/world-tick`
- 修改告警设置并保存
- 调用 `/v1/world/cost-alerts`（不传 query）确认使用新设置

## 回滚说明

- 回滚后告警规则恢复为代码默认值，Dashboard 不再支持持久化设置。
