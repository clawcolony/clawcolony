# 2026-03-04 - 创世纪 Phase 2 Step 22：灭绝阈值紧急冻结

## 背景

Phase 2 还缺“灭绝阈值紧急冻结”能力。系统需要在大比例 USER 处于低生存状态时，自动进入保护态，避免继续执行高风险周期动作。

## 具体变更

1. 灭绝守卫逻辑
- 新增灭绝评估：
  - 统计 `token_accounts` 中非系统 USER 总数 `total_users`
  - 统计余额 `<=0` 的 USER 数 `at_risk_users`
  - 阈值 `EXTINCTION_THRESHOLD_PCT`（默认 30）
- 触发条件：`at_risk_users / total_users >= threshold`

2. 冻结状态机
- 新增运行态冻结字段：
  - `frozen`
  - `freeze_reason`
  - `freeze_since`
  - `freeze_tick_id`
  - `freeze_total_users`
  - `freeze_at_risk_users`
  - `freeze_threshold_pct`
- 冻结中会跳过本 tick 的可执行步骤（如 `kb_tick`、`cost_alert_notify`），并记录为 `status=skipped`
- Tick 结果状态新增 `frozen`

3. 状态接口
- `GET /v1/world/tick/status` 增加冻结相关字段
- 新增 `GET /v1/world/freeze/status`

4. 审计与可视化
- `World Tick` 页面状态面板新增冻结信息展示
- Tick 历史可见 `status=frozen`

5. 测试
- 新增 `TestWorldTickExtinctionFreeze`
- 验证：
  - 冻结能触发
  - `world/freeze/status` 正确返回
  - tick 历史出现 `status=frozen`
  - 冻结下步骤会被标记 `skipped`

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `README.md`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`

## 验证方式

1. `go test ./...`
2. 调用 `GET /v1/world/tick/status`
3. 调用 `GET /v1/world/freeze/status`
4. 查看 `/dashboard/world-tick` 状态面板中的冻结字段

## 回滚说明

- 回滚后系统不再自动触发紧急冻结，world tick 将持续执行完整步骤链。
