# 2026-03-03 OpenClaw Register 改为异步任务 + Dashboard 可视化

## 背景

`POST /v1/openclaw/admin/action` 的 `action=register` 原为同步执行，流程包含 GitHub API、仓库同步、镜像构建与部署，容易导致请求长时间阻塞。

## 本次变更

1. `action=register` 改为异步任务
- 提交后立即返回：
  - `register_task_id`
  - `status=running`
- 后台 goroutine 执行完整 provisioning 与部署。

2. 新增任务查询接口
- `GET /v1/openclaw/admin/register/task?register_task_id=<id>`
  - 返回任务状态、最后步骤、步骤总数、完整 steps。
- `GET /v1/openclaw/admin/register/history?limit=<n>`
  - 返回最近 register 任务历史列表。

3. 持久化表（PostgreSQL）
- `register_tasks`
- `register_task_steps`

4. Dashboard（OpenClaw Pods 页面）
- 创建成功后显示 `register_task_id`。
- 新增“Register 任务”区域：
  - 可输入 task id 查询。
  - 实时显示步骤日志与状态摘要。
  - 显示最近任务历史，可点击 task id 快速查看详情。

## 兼容性

- `POST /v1/openclaw/admin/action` 的 `register` 请求参数不变。
- 客户端需从“同步等待结果”切换为“拿 task id 后轮询”。
