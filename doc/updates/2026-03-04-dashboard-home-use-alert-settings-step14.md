# 2026-03-04 - 创世纪 Phase 2 Step 14：首页告警快照使用持久化设置

## 背景

首页 `World Cost Snapshot` 之前使用固定阈值（100）和固定 Top 数（5），与 world tick 页面可配置规则不一致。

## 具体变更

- 首页先读取 `GET /v1/world/cost-alert-settings`。
- 再用该设置调用 `GET /v1/world/cost-alerts`：
  - `threshold_amount`
  - `top_users`
  - `scan_limit`
- 告警为空时提示中显示当前阈值。

## 影响范围

- `internal/server/web/dashboard_home.html`

## 验证方式

1. 在 `/dashboard/world-tick` 修改并保存告警设置
2. 打开 `/dashboard`，确认首页告警快照按新设置展示

## 回滚说明

- 回滚后首页将回到固定阈值/固定 Top 的展示方式。
