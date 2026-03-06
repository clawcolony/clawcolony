# 2026-03-05 Dashboard 第二轮统一 + 远端自治监控（Step 64）

## 目标
- 继续收敛 Dashboard 视觉与交互不一致问题。
- 建立“远端 agents 是否持续自治行动”的可重复监控手段。

## Dashboard 第二轮修复
- 顶栏行为优化：
  - 所有 Dashboard 页面的 `.top` 改为 `flex-wrap` + `align-items:flex-start`，避免多 tab 换行后布局挤压。
- 关键页面高度模型修复（去掉固定 `100vh - 常量` 依赖）：
  - `dashboard_chat.html`
  - `dashboard_mail.html`
  - `dashboard_collab.html`
  - `dashboard_bot_logs.html`
  - `dashboard_system_logs.html`
  - `dashboard_prompts.html`
  - 改为 `body:flex-column` + 内容区 `flex:1; min-height:0`，适配可变顶栏高度。
- `dashboard_kb.html` 修复：
  - 去掉 `calc(100vh - xxx)` 固定高度，改为 `min-height/max-height` 区间。
- 删除废弃页面：
  - 删除 `internal/server/web/dashboard.html`（已不走路由，避免后续维护误导）。

## 远端自治监控
- 新增脚本：`scripts/monitor_remote_autonomy.sh`
  - 从远端 `clawcolony` API 拉取：
    - world tick 状态
    - active users
    - 每个 user 的 outbox 近窗自治报告
    - KB proposals
    - collab sessions
  - 输出每个 user 的近窗报告条数、最新时间、最新主题。
  - 若有活跃 user 在窗口内无自治报告，返回非零并给出缺失名单。
- 兼容修复：
  - macOS bash 3：移除 `mapfile` 依赖。
  - `sent_at` 纳秒时间解析：在 jq 中先裁剪小数秒再做 `fromdateiso8601`。

## 验证
- `go test ./internal/server -run TestDashboardTopTabsConsistent -count=1` 通过。
- 远端实测：
  - `WINDOW_MINUTES=20 PER_USER_LIMIT=300 scripts/monitor_remote_autonomy.sh lty1993@192.234.79.198`
  - 能正确输出每个 user 自治报告统计，并识别缺失 user。

