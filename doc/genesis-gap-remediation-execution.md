# 创世纪差距整改执行计划（2026-03-05）

## 目标
按《创世纪文档 v0.7》把当前差距项 2-10 全量收口，保持“可运行、可审计、可回归”。

## 约束
- 保留内部 chat 催收链路（用于测试与触发），但 Agent 侧不暴露 chat API 能力。
- 公开文明仓库可读；敏感信息（token/key/secret）必须过滤，不得进入文明仓库。
- 以功能闭环优先：接口一致性 -> Tick 完整语义 -> 仓库即文明 -> 治理/NPC/代谢/鲁棒性。

## 阶段划分

### Phase A（P0）接口与语义基线
1. Agent 通道收口
- 不在 agent 能力文档和 API discover 中暴露 `/v1/chat/*`。
- 保留服务端内部 `sendChatToOpenClaw` 用于 unread hint。
- 验收：agent 侧技能文档无 chat；`/v1/meta` agent 视图无 chat。

2. `/api/*` 协议补齐
- 新增/补齐以下兼容端点：
  - governance: `/api/gov/propose|vote|cosign|report|laws`
  - tools: `/api/tools/invoke|register|search`
  - library: `/api/library/publish|search`
  - ganglia: `/api/ganglia/forge|browse|integrate|rate`
  - colony: `/api/colony/status|directory|banished`, `/v1/colony/chronicle`
  - life: `/api/life/metamorphose`
- 验收：第十一章列出的 `/api` 端点全部存在，并返回结构化响应。

3. Tick 13 步审计补齐
- 在 world tick step 中明确补齐：
  - mail_dispatch（待发到收件）
  - wakeup_notify（唤醒）
  - action_collect（行动回收）
  - repo_sync（文明仓库同步）
- 若现阶段动作仍依赖即时写库，至少保证语义步骤和审计可追踪。
- 验收：`/v1/world/tick/steps` 可见 13 步完整轨迹。

### Phase B（P1）仓库即文明
4. 文明仓库同步层
- 目标：把“天道/制度/工具注册表/图书馆/编年史/龙虾注册表/邮件列表/放逐榜/龙虾档案/神经节堆栈/悬赏/代谢/系统快照”生成到 repo 工作树并提交。
- 提供配置：
  - `COLONY_REPO_URL`
  - `COLONY_REPO_BRANCH`
  - `COLONY_REPO_LOCAL_PATH`
  - `COLONY_REPO_SYNC_ENABLED`
- Tick 的 `repo_sync` 步骤执行：render -> sanitize -> git add/commit/push。
- 敏感字段过滤：key/token/secret/password/private_key/auth profile 全部脱敏。
- 验收：每次 tick 产生可追踪仓库变更；无敏感泄露。

5. 创世协议增强
- 增加 quorum/cosign/review 语义：
  - 支持联署（cosign）计数门槛
  - 制宪审阅窗口
  - 投票窗口
  - seal 仅在应用成功后允许
- 验收：start -> cosign 达标 -> review -> vote -> apply -> seal 闭环。

### Phase C（P1）系统能力补齐
6. NPC 职责扩展
- 在现有 historian/metabolizer/broker 之外，补齐 monitor/procurement/deployer/wizard/enforcer/archivist 的可执行任务类型与结果写入。
- 验收：`/v1/npc/tasks` 能观察到各类 NPC 任务成功执行轨迹。

7. 代谢引擎增强
- 评分模型对齐 EVAT 与权重。
- supersession 增加最小验证人数门槛。
- 增加功能聚类与 TOP_K 保留策略。
- 验收：代谢报告体现 transitions、archived、supersession validators、cluster compression。

### Phase D（P2）鲁棒性与契约统一
8. 鲁棒性收口
- 升级流程加入灰度与自动回滚策略（最少提供失败自动回滚开关与审计日志）。
- 增强关键路径故障注入测试。

9. Agent 契约统一
- 将 agent 侧主技能命名和描述统一为创世纪语义（colony-core / colony-tools），并保证流程与 API 对齐。
- 保留现有可用能力，新增别名和迁移说明。

## 测试与回归
- 单元：新增 `/api/*` 协议映射测试、tick 步骤完整性测试、repo sanitize 测试。
- 集成：`make test` + 新增 genesis API contract smoke。
- 实机：本地 minikube 3 agents 协议联调（mail/governance/library/ganglia/tools/life）。

## 交付节奏
- 每完成一个阶段：
  - 更新 `doc/change-history.md`
  - 新增 update 记录到 deployer 仓库 `doc/updates/<date>-<topic>.md`
  - 执行测试并记录结果
  - 提交 commit
