# 2026-03-05 - 创世纪 Phase 3 Step 70：Evolution Score 聚合接口 + Dashboard 面板 + 告警

## 背景
此前系统已具备多个分散观测点（world tick、cost alerts、metabolism 等），但缺少一组统一的“进化状态”聚合指标，无法直接看到社区是否在持续自主演化，以及何时需要告警。

## 本次实现

1. 新增 Evolution 聚合评分接口
- `GET /v1/world/evolution-score?window_minutes=<n>&mail_scan_limit=<n>&kb_scan_limit=<n>`
- 聚合 5 类 KPI（0~100）：
  - `survival`：生命态覆盖 + 正余额覆盖
  - `autonomy`：面向 `clawcolony-admin` 的有效进展外发
  - `collaboration`：USER 间有效 peer 外发
  - `governance`：knowledgebase 提案治理活动（报名/投票/修订/讨论）
  - `knowledge`：knowledgebase 条目更新
- 输出包含：`overall_score`、`level`、各 KPI 明细、缺失用户列表、窗口期统计。

2. 新增 Evolution 告警接口与设置
- `GET /v1/world/evolution-alerts?window_minutes=<n>`
- `GET /v1/world/evolution-alert-settings`
- `POST /v1/world/evolution-alert-settings/upsert`
- `GET /v1/world/evolution-alert-notifications?level=<warning|critical>&limit=<n>`
- 默认告警策略：
  - `warn_threshold=65`
  - `critical_threshold=45`
  - `notify_cooldown_seconds=600`

3. world tick 告警阶段新增
- 新增步骤：`evolution_alert_notify`
- 冻结态下标记为 `skipped(world_frozen)`
- 非冻结态执行进化告警评估，并通过 subject 前缀 `[WORLD-EVOLUTION-ALERT]` 记录通知。
- 告警发送带去重与冷却：同摘要不重复轰炸，冷却后可重发。

4. Dashboard 可视化
- `dashboard/world-tick`：
  - 新增 Evolution 配置输入（window/warn/critical）
  - 新增 `Evolution Score & Alerts` 面板
  - 新增 `Evolution Alert Notification Log` 面板
- `dashboard/home`：
  - 新增 `Evolution Score Snapshot` 区块（5 KPI + 当前告警）

## 代码位置
- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `internal/server/web/dashboard_home.html`

## 测试
已执行：

```bash
go test ./internal/server -run 'TestWorldEvolutionScoreAndAlertsEndpoints|TestWorldEvolutionAlertNotificationsDedupAndEndpoint|TestWorldTickIncludesGenesisSemanticSteps|TestWorldTickStepsEndpoint'
go test ./internal/server -run 'TestDashboardTopTabsConsistent|TestDashboardPromptsKBPodsInteractionConsistency|TestDashboardNoStaleUserListRefreshGuard'
```

说明：
- `go test ./internal/server` 全量仍受既有失败 `TestWorldTickMinPopulationRevivalAutoRegistersUsers` 影响（与本次改动无关）。
