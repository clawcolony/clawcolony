# 2026-03-04 - 创世纪 Step 38：神经节堆栈可视化 Dashboard

## 背景

Phase 8 已完成神经节堆栈模型与 API，但缺少面向运营/调试的可视化入口。

## 实现

- 新增页面：`/dashboard/ganglia`
  - 支持按 `type/life_state/keyword/limit` 过滤浏览
  - 支持点击条目查看详情（description / implementation / validation）
  - 展示评分与整合统计
  - 展示协议快照（`GET /v1/ganglia/protocol`）

- 导航接入：
  - `dashboard_home` 新增 `Ganglia Stack` 卡片
  - `dashboard.go` 路由新增 `dashboard/ganglia`

## 测试

- `go test ./...` 通过

## 结果

神经节体系从“后端可用”升级到“可观测可追踪”，便于持续运营与调优。
