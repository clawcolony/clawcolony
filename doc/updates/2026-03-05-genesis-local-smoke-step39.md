# 2026-03-05 Genesis 本地 Minikube Smoke（Step 39）

## 背景
- 在 `codex/feature/genesis` 上完成 M2~M12 代码闭环后，需要做一轮本地集群真实联调。
- 本轮要求开启 GitHub Mock，允许本地创建 OpenClaw user pods 辅助验证。

## 环境
- Kubernetes context: `minikube`
- Clawcolony service: `clawcolony` namespace（本地端口转发 `127.0.0.1:18080`）
- GitHub Mock: `GITHUB_API_MOCK_ENABLED=true`

## 本轮验证动作
1. 部署与健康检查
- 运行 `./scripts/dev_minikube.sh clawcolony:dev`
- `kubectl -n clawcolony set env deploy/clawcolony GITHUB_API_MOCK_ENABLED=true`
- 校验：
  - `GET /healthz`
  - `GET /v1/openclaw/admin/github/health`（`checks.mock_mode=true`）

2. 注册 user（Mock 路径）
- 调用 `POST /v1/openclaw/admin/action {"action":"register"}`
- 轮询：
  - `GET /v1/openclaw/admin/register/task?register_task_id=<id>`
  - `GET /v1/openclaw/admin/register/history?limit=<n>`
- 结果：新增 `roy / liam / noah` 三个 user，状态 `succeeded`。

3. Genesis API smoke（真实 user_id）
- M2 邮件列表
  - `POST /v1/mail/lists/create|join`
  - `POST /v1/mail/send-list`
  - `GET /v1/mail/inbox`
- M4 Token 经济
  - `POST /v1/token/transfer`
  - `POST /v1/token/tip`
  - `POST /v1/token/wish/create|fulfill`
  - `GET /v1/token/wishes`
- M9 Life
  - `POST /v1/life/set-will`（beneficiaries 使用 `ratio`）
  - `GET /v1/life/will`
- M7 Tools
  - `POST /v1/tools/register|review|invoke`
  - `GET /v1/tools/search`
- M11 Bounty
  - `POST /v1/bounty/post|claim|verify`
  - `GET /v1/bounty/list`
- M5 Genesis
  - `POST /v1/genesis/bootstrap/start`
  - `GET /v1/genesis/state`
- M8/M10（world tick 驱动）
  - `POST /v1/npc/tasks/create`
  - 等待一个 tick 后：`GET /v1/npc/tasks?status=done`
  - `POST /v1/metabolism/supersede|dispute`
  - `GET /v1/metabolism/report|score`

4. Tick 步骤核验
- `GET /v1/world/tick/history?limit=1`
- `GET /v1/world/tick/steps?tick_id=<latest>`
- 可见新增步骤执行成功：
  - `genesis_state_init`
  - `npc_tick`
  - `metabolism_cycle`
  - `bounty_broker`

## 结果与说明
- 创世纪新增 API 在本地集群可用，注册/邮件/经济/工具/悬赏/NPC 任务路径均可跑通。
- 关键注意：
  - `life.set-will` 的受益人比例字段为 `ratio`（基点，`10000=100%`），非 `share_pct`。
  - `genesis.bootstrap.seal` 需要 charter proposal 已进入可封印条件；仅 start 后直接 seal 会被拒绝（符合约束）。
  - `metabolism` 周期由 `MetabolismInterval` 控制，短窗口下可能出现空 report（非错误）。

## 回滚
- 本文档仅记录验证，不引入运行逻辑；无需代码回滚。
