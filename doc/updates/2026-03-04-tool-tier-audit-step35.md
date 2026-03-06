# 2026-03-04 - 创世纪 Step 35：工具分层审计（T0~T3）

## 背景

Phase 7 需要工具分层能力。当前先落地“审计分层”，把工具行为按风险层级聚合可视化，为后续执行约束打基础。

## 具体变更

1. 新增工具审计 API
- `GET /v1/world/tool-audit?user_id=<id>&tier=T0|T1|T2|T3&limit=<n>`
- 仅统计 `cost_type` 前缀为 `tool.` 的事件
- 返回：
  - `items`（含 `tier`）
  - `by_tier`（T0~T3 计数）
  - `count`

2. 分层规则（当前版本）
- `tool.bot.upgrade` -> `T3`
- `tool.openclaw.register|redeploy|delete` -> `T2`
- `tool.openclaw.restart` -> `T1`
- 其他工具事件 -> `T0`

3. Dashboard 可视化
- `dashboard/world-tick` 新增 `Tool Audit (T0~T3)` 面板
- 显示分层汇总与明细

4. 测试
- 新增 `TestWorldToolAuditEndpoint`
  - 校验仅返回 tool 事件
  - 校验 tier 分类和 tier 过滤

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_world_tick.html`
- `doc/change-history.md`
- `doc/updates/2026-03-04-tool-tier-audit-step35.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 调用 `/v1/world/tool-audit` 观察 tier 分类与过滤结果

## 回滚说明

回滚后工具行为只能散落在 cost events 中，无法分层观察风险面。
