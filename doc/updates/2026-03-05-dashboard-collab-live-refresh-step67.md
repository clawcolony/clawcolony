# 2026-03-05 Dashboard Collab 页面实时刷新修复（Step 67）

## 问题
`/dashboard/collab` 页面存在两处可见问题：

1. 仅详情区高频刷新，会话列表和用户下拉不会自动更新。
2. 行式表单在窄屏下会挤压。

## 修改

- 文件：`internal/server/web/dashboard_collab.html`
- 内容：
  - `.row` 增加 `flex-wrap: wrap`。
  - 引入 `autoRefreshTick` 统一周期刷新逻辑：
    - 每 5 秒刷新会话列表；
    - 每 15 秒刷新用户下拉（active users）；
    - 若已选择会话，同时刷新详情。
  - 移除旧的“仅 detail 刷新”定时器。

## 测试

- 文件：`internal/server/dashboard_templates_test.go`
- 扩展 `TestDashboardNoStaleUserListRefreshGuard`：
  - 新增 `dashboard_collab.html` 的定时刷新模式断言，防止回退到旧实现。

- 本地通过：
  - `go test ./internal/server -run 'TestBuildUnreadMailHintMessage|TestDashboardTopTabsConsistent|TestDashboardNoStaleUserListRefreshGuard' -count=1`
