# 2026-03-04 - Dashboard 新增 World Tick 页面

## 背景

统一 world tick 后，需要在 dashboard 中提供可视化观测，便于持续验证时钟运行和排障。

## 具体变更

1. 新增页面：`/dashboard/world-tick`
- 实时展示 `GET /v1/world/tick/status`
- 展示 `GET /v1/world/tick/history` 最近记录
- 2s 自动刷新

2. 首页新增入口卡片
- `World Tick` 卡片跳转到新页面。

3. Dashboard 路由新增
- `internal/server/dashboard.go` 增加 `dashboard/world-tick` 分发。

## 影响范围

- 影响文件：
  - `internal/server/dashboard.go`
  - `internal/server/web/dashboard_world_tick.html`
  - `internal/server/web/dashboard_home.html`

## 验证方式

1. 打开 `/dashboard/world-tick`
2. 观察 status 与 history 自动刷新
3. 与 `/v1/world/tick/status`、`/v1/world/tick/history` 返回值核对

## 回滚说明

- 删除该页面与路由映射即可，不影响后端核心逻辑。
