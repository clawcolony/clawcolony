# 2026-03-04 - 创世纪 Phase 2 Step 19：告警通知冷却时间配置化

## 背景

此前高消耗告警通知节流窗口固定为 10 分钟，无法根据运行阶段动态调整。

## 具体变更

1. 告警设置新增字段
- 在 `world_cost_alert_settings` 中新增 `notify_cooldown_seconds`
- 默认值：`600`
- 服务端归一化规则：`[30, 86400]`

2. 告警通知逻辑改为读取配置
- world tick 告警通知发送流程改为读取 `notify_cooldown_seconds`
- 去重/节流判定改为按该配置执行（金额上升仍可立即再次通知）

3. Dashboard 设置面板增强
- `World Tick` 页面新增 `alert cooldown(s)` 输入项
- 保存设置时会随同其它告警参数一起提交

4. 测试
- 更新 `TestWorldCostAlertSettingsEndpoints`：
  - 验证默认返回 `notify_cooldown_seconds=600`
  - 验证 upsert 后可读回指定值
- 更新 `TestWorldCostAlertSettingsUpsertNormalizesInvalidValues`：
  - 验证过小值会被归一化到 `30`

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. `GET /v1/world/cost-alert-settings`，确认包含 `notify_cooldown_seconds`
3. `POST /v1/world/cost-alert-settings/upsert` 写入冷却秒数并回读确认
4. 打开 `/dashboard/world-tick`，确认可编辑并保存 `alert cooldown(s)`

## 回滚说明

- 回滚后告警冷却时间恢复为固定值（10 分钟），Dashboard 不再可配置该项。
