# 2026-03-01 Async Upgrade Task + Dashboard Polling

## 变更摘要
- 将 `POST /v1/bots/upgrade` 改为异步执行：接口立即返回 `upgrade_task_id`，升级在后台执行。
- 新增 `GET /v1/bots/upgrade/task?upgrade_task_id=<id>`：用于查询升级任务当前状态、最近步骤与步骤数量。
- Dashboard `Prompt Templates` 页面新增“异步分支升级”区块：
  - 选择目标 USER + branch 发起升级
  - 显示 `upgrade_task_id`
  - 自动轮询任务状态（3 秒）直到成功/失败

## 设计说明
- 任务 ID 直接复用 `upgrade_audits.id`，避免引入新表和额外映射。
- 升级互斥仍以 `user_id` 维度保留（同一 USER 同时仅允许一个升级任务）。
- 状态查询返回 `audit` + `last_step` + `step_count`，满足 Dashboard 快速反馈场景。

## 兼容性
- 旧调用方若依赖 `POST /v1/bots/upgrade` 同步返回最终结果，需要改为：
  1) 先调用 `POST` 获取 `upgrade_task_id`
  2) 再轮询 `GET /v1/bots/upgrade/task`
- 历史接口与步骤接口保持不变：
  - `GET /v1/bots/upgrade/history`
  - `GET /v1/bots/upgrade/steps`

## 测试
- `go test ./...` 通过。
