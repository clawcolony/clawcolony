# 2026-03-03 Register 任务阶段化与进度可视化

## 目标

让 `openclaw register` 这类长流程具备更清晰的分阶段状态和日志，便于在 Dashboard 中观测卡点。

## 变更

1. Register 任务步骤改为显式阶段日志
- 关键阶段写入 `running/ok/failed/warn`：
  - `ensure_repo`
  - `sync_repo`
  - `generate_git_credentials`
  - `deploy_key`
  - `upsert_git_secret`
  - `build_image`
  - `register_and_deploy`

2. 任务查询接口新增阶段与进度
- `GET /v1/openclaw/admin/register/task?register_task_id=<id>`
  - 新增返回字段：
    - `phase`
    - `progress`（0-100）

3. Dashboard 显示增强
- Register 任务详情中增加：
  - `phase`
  - `progress`
- 结合 steps 可直接观察当前运行阶段与最后一步状态。

## 效果

- 对“请求长时间运行”的场景，能快速判断是在 GitHub、凭据、构建还是部署阶段卡住。
