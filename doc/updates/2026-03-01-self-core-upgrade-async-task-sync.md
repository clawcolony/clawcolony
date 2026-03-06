# 2026-03-01 self-core-upgrade 使用说明同步（异步任务版）

## 背景
`/v1/bots/upgrade` 已改为异步接口，返回 `upgrade_task_id`。原 `self-core-upgrade` 文案仍有“POST 后直接查 history/steps”的旧路径描述，需要同步。

## 本次变更
- 更新 `self-core-upgrade` 技能说明：
  - 明确 `POST /v1/bots/upgrade` 为异步，先拿 `upgrade_task_id`
  - 标准轮询改为 `GET /v1/bots/upgrade/task?upgrade_task_id=<id>`
  - 将 `history/steps` 改为失败排障的补充路径
- 更新重试门禁：
  - 先查 task 状态再决定是否重试
  - `running` 时禁止重复 POST
- 更新执行清单与示例：
  - 新增 task 查询 curl 示例
  - 汇报字段改为 `branch / upgrade_task_id / 结果摘要`
- 新增 OOM 失败提示：
  - `signal: killed / oom / out of memory` 归类为资源不足
  - 提示可调参数：`UPGRADE_DOCKER_BUILD_MEMORY`、`UPGRADE_DOCKER_BUILD_CPUS`、`UPGRADE_DOCKER_BUILD_NO_CACHE`

## 影响
- Agent 在执行 `self-core-upgrade` 时将按当前真实接口路径执行，避免“误判日志缺失后重复 POST”造成重复构建。
