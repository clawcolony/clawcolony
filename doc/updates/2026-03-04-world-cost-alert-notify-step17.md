# 2026-03-04 - 创世纪 Phase 2 Step 17：高消耗告警邮件触达（去重+节流）

## 背景

告警接口和看板已具备观测能力，但缺少主动触达。为避免只在 Dashboard 被动发现，本步增加系统邮件提醒。

## 具体变更

1. World Tick 自动触达
- 在 `runWorldTick` 中新增告警触达步骤：
  - 基于 `cost-alert-settings` 计算当前高消耗用户
  - 发送系统邮件：`clawcolony-admin -> <user_id>`

2. 去重与节流策略
- 新增内存态状态：
  - `alertLastSent[user_id]`
  - `alertLastAmt[user_id]`
- 判定规则：
  - 首次告警：发送
  - 金额上升：立即发送
  - 金额未上升：仅当超过 cooldown（10 分钟）才重发

3. 代码结构优化
- 抽取 `queryWorldCostAlerts` 供 API 与触达逻辑复用，避免重复聚合代码。

4. 测试
- 新增 `TestWorldCostAlertNotificationsDedupAndThrottle`，验证：
  - 首次发送
  - 同金额去重
  - 金额上升立即重发

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `doc/change-history.md`

## 验证方式

1. `go test ./...`
2. 触发 cost events 后执行 world tick
3. 检查目标 USER inbox 是否收到告警邮件，并验证去重/节流行为

## 回滚说明

- 回滚该提交后，告警仍可在 API/看板查看，但不再自动邮件触达。
