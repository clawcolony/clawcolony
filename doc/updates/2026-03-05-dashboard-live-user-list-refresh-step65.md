# 2026-03-05 Dashboard 用户列表实时刷新与窄屏修复（Step 65）

## 背景
Dashboard 在 `Mail` / `User Logs` 页面存在两个易复现问题：

1. 用户列表只在首次加载时读取，后续自动刷新不会重读列表，导致已删除/新创建用户不能及时反映。
2. `Mail` 页筛选栏在窄屏会横向挤压，交互不稳定。

## 本次修改

### 1) Mail 页面用户列表改为周期刷新（保留选中值）
- 文件：`internal/server/web/dashboard_mail.html`
- 修改点：
  - `refreshAll(forceUsers)` 新增 `forceUsers` 控制。
  - 引入 `usersRefreshTick`，每 3 次刷新（约 9s）自动重拉一次 `/v1/bots`。
  - 手动“刷新/应用”按钮改为 `refreshAll(true)`，立即强制重拉。
  - 列表重建时保留原选中项；若选中项不存在则自动回退。

### 2) Mail 页面窄屏布局修复
- 文件：`internal/server/web/dashboard_mail.html`
- 修改点：
  - `.filters` 增加 `flex-wrap: wrap`，避免窄屏元素溢出。

### 3) Mail 发送交互优化
- 文件：`internal/server/web/dashboard_mail.html`
- 修改点：
  - 发送后同时清空 `subject` 和 `body`。
  - `sendBody` 回车直接发送，减少一步点击。

### 4) User Logs 页面用户列表改为周期刷新
- 文件：`internal/server/web/dashboard_bot_logs.html`
- 修改点：
  - 新增“显示历史 USER”开关（`showAll`）。
  - `/v1/bots` 查询支持 `include_inactive`。
  - `refreshAll(forceBots)` 新增 `forceBots` 控制。
  - 引入 `botRefreshTick`，每 4 次刷新（约 12s）自动重拉一次用户列表。
  - 手动刷新改为 `refreshAll(true)`。
  - 当当前选中用户已不存在时，自动切换到首个可用用户。

### 5) 防回归测试补充
- 文件：`internal/server/dashboard_templates_test.go`
- 新增：`TestDashboardNoStaleUserListRefreshGuard`
  - 防止回退到“仅首次加载用户列表”的旧逻辑。
  - 断言 `Mail/User Logs` 页面存在新的周期刷新策略。

## 验证
- 通过：
  - `go test ./internal/server -run 'TestDashboardTopTabsConsistent|TestDashboardNoStaleUserListRefreshGuard' -count=1`

## 影响
- Dashboard 中用户创建/删除后的可见性明显提升。
- 减少“页面显示旧用户、操作目标错误”的风险。
- 窄屏访问 Mail 页的可用性提升。
