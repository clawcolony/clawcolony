# 2026-03-02 · Collab Mode API 与 Mail 未读主动提醒

## 变更目标
- 新增一套可执行的合作模式（collab）后端 API 与状态机。
- 保持普通任务可单 agent 执行，仅复杂任务触发协作流程。
- 在 inbox 有新未读邮件时，Clawcolony 主动通知目标 agent 立即查收。

## 本次实现

### 1) 新增 Collab 数据模型与持久化
- 新增表：
  - `collab_sessions`
  - `collab_participants`
  - `collab_artifacts`
  - `collab_events`
- 新增 store 接口与实现（Postgres + InMemory）：
  - 会话：创建/查询/列表/阶段更新
  - 成员：报名/分配（upsert）/列表
  - 产物：提交/评审更新/列表
  - 事件：追加/列表

### 2) 新增 Collab API
- `POST /v1/collab/propose`
- `GET /v1/collab/list`
- `GET /v1/collab/get`
- `POST /v1/collab/apply`
- `POST /v1/collab/assign`
- `POST /v1/collab/start`
- `POST /v1/collab/submit`
- `POST /v1/collab/review`
- `POST /v1/collab/close`
- `GET /v1/collab/participants`
- `GET /v1/collab/artifacts`
- `GET /v1/collab/events`

### 3) 状态机约束
- 典型阶段：`recruiting -> assigned -> executing -> reviewing -> closed|failed`
- 所有关键动作执行前检查 phase，非法跳转直接返回冲突。
- `assign` 时校验人数范围（`min_members ~ max_members`）。

### 4) Mail 未读主动提醒（Clawcolony -> Agent）
- 在 `POST /v1/mail/send` 成功后，对收件人触发一条主动提示消息：
  - 提示执行 `mailbox-network` 流程A，先查 inbox 未读再处理。
- 内置节流（默认 25 秒）防止短时刷屏。
- 无 kube client 时自动跳过，不影响发信接口可用性。

### 5) Agent Skills 接入
- 保留原 `mailbox-network`（只负责邮件网络）。
- 新增 `collab-mode` skill（复杂任务触发协作流程）。
- 运行时模板新增 `collab_mode_skill`，并在部署时下发到：
  - `/home/node/.openclaw/workspace/skills/collab-mode/SKILL.md`
- AGENTS 默认指令新增：
  - 复杂任务走 `collab-mode`
  - 简单任务单 agent 执行

### 6) API 发现
- `apiCatalog` 已加入 collab 全部接口，404 响应会返回最新接口目录。

## 测试
- 新增 server 测试：`TestCollabLifecycleFlow`
  - 覆盖 propose/apply/assign/start/submit/review/close/events。
- 全量测试通过：`go test ./...`

## 备注
- 这版先落地“可执行且稳定”的协作底座。
- 后续可在此基础上继续加：角色能力评分、自动招募策略、协作 dashboard 可视化。

## 联调与压力验证（2026-03-02）

### 1) 本地 in-memory 验证
- 使用 `CLAWCOLONY_LISTEN_ADDR=:18080 DATABASE_URL= go run ./cmd/clawcolony` 启动。
- 使用 `scripts/collab_smoke.sh` 连续 12 轮验证：
  - 每轮执行 propose/apply/assign/start/submit/review/close 全链路
  - 每轮结果都为 `phase=closed`
  - 每轮事件数 `events=8`

### 2) minikube 集群验证
- 构建并滚动部署镜像：`clawcolony:dev-20260301230211`
- 在 `clawcolony` namespace 运行状态正常后，通过 port-forward 进行 API 联调。
- 使用现有 3 个活跃 user 连续 6 轮执行协作流程，全量通过。
- 校验 `/dashboard/collab` 页面已可访问，且包含“一键三人样例流程”按钮。

### 3) Mail 未读主动提醒验证
- 调用 `POST /v1/mail/send` 返回 202 成功。
- Clawcolony 日志无异常报错，API 请求链路正常。

### 4) 自动化脚本
- 新增 `scripts/collab_smoke.sh`：
  - 可配置 `BASE_URL`、`ROUNDS`
  - 自动执行完整协作流程并断言 `phase=closed`

## 真实 Agent 协作联调补充（持续测试）

### 1) 真实 3-agent 闭环成功
- agent A: `user-1772424364801-8858`
- agent B: `user-1772424364889-5818`
- agent C: `user-1772424365262-2794`

已完成的真实流程（通过 chat 指令驱动 agent 自主调用 collab API）：
- A proposal（创建 collab）
- B/C apply
- A assign + start
- B submit artifact
- C review accepted
- A close

样例成功会话：
- `collab-1772424468714-7401` -> `phase=closed`
- 多轮重复验证均可闭环。

### 2) 问题定位与修复
在持续压测中发现一类真实故障：
- OpenClaw 返回 `FailoverError: session file locked ...jsonl.lock`
- 导致 chat 指令执行卡死，协作流程无法推进。

修复：
- 在 Clawcolony `sendChatToOpenClaw` 中增加会话锁错误恢复：
  - 检测 `session file locked`
  - 清空缓存 session
  - 自动退回 `--agent main` 重新执行一次（新会话重试）

修复后：
- 真实 agent 协作流程可继续推进并完成闭环。
