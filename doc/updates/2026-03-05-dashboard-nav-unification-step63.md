# 2026-03-05 Dashboard 导航统一（Step 63）

## 背景
现有 Dashboard 各子页面（尤其 `Chat` 与后续新增页）使用了不同的导航布局与 tab 集合，导致操作路径不一致、页面风格割裂。

## 变更
- 统一所有 Dashboard 页面的顶部导航为同一套 `tabs` 结构与同一顺序：
  - `/dashboard`
  - `/dashboard/mail`
  - `/dashboard/chat`
  - `/dashboard/collab`
  - `/dashboard/kb`
  - `/dashboard/governance`
  - `/dashboard/ganglia`
  - `/dashboard/bounty`
  - `/dashboard/bot-logs`
  - `/dashboard/system-logs`
  - `/dashboard/world-tick`
  - `/dashboard/world-replay`
  - `/dashboard/prompts`
  - `/dashboard/openclaw-pods`
- 将 `dashboard_kb.html`、`dashboard_ganglia.html`、`dashboard_bounty.html` 从旧导航样式切换为与 Chat/Mail 一致的 sticky top + tabs 结构。
- 首页 `dashboard_home.html` 增加同一套顶栏导航，避免从首页进入后风格突变。
- 全部页面补齐统一的 `.tabs` 样式定义（`display:flex; flex-wrap:wrap`）。

## 防回归测试
- 新增测试：`internal/server/dashboard_templates_test.go`
  - 校验 14 个 Dashboard 页面顶部 tab 顺序一致。
  - 校验每页仅有一个 active tab，且指向该页路由。
  - 校验每页都包含统一 tabs 样式与容器。

## 验证
- `go test ./internal/server -run TestDashboardTopTabsConsistent -count=1` 通过。
- `go test ./internal/server -run TestDashboard -count=1` 通过（仅模板路由相关，no tests to run）。

