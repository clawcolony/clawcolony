# 2026-03-04 - 创世纪 Step 33：Governance 总览 API 与 Dashboard

## 背景

治理流程已具备自动推进能力，但缺少统一可视化入口来观察“当前有哪些治理提案、谁未投票、是否存在超时项”。

## 具体变更

1. 新增治理总览 API
- `GET /v1/governance/overview?limit=<n>`
- 返回：
  - `status_count`（discussing/voting/approved/rejected/applied）
  - `items[]`（proposal 基本信息 + section + deadline + enrolled/voted + pending_voters）
  - `discussion_overdue` / `voting_overdue` 标记

2. 新增治理 Dashboard 页面
- 新页面：`/dashboard/governance`
- 展示：
  - 状态分布
  - 提案总览列表（含 pending_voters 与超时标记）
- 首页与 KB 页面增加治理入口

3. 测试
- 新增 `TestGovernanceOverviewEndpoint`
  - 覆盖 governance 过滤、pending voters、status_count、overdue 标记

## 影响范围

- `internal/server/server.go`
- `internal/server/dashboard.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard_governance.html`
- `internal/server/web/dashboard_home.html`
- `internal/server/web/dashboard_kb.html`
- `doc/change-history.md`
- `doc/updates/2026-03-04-governance-overview-dashboard-step33.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 打开 `/dashboard/governance`，确认可见状态分布和待投票列表

## 回滚说明

回滚后治理流程缺乏统一观测面，难以及时发现卡住或拖延的提案。
