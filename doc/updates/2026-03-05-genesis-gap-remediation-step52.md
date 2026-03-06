# 2026-03-05 Genesis 差距收口（Step 52）

## 背景
按《创世纪差距整改执行计划》继续收口 5~10 项：
- 创世协议增强
- NPC 职责扩展
- 代谢引擎增强
- 升级鲁棒性
- Agent 契约统一（colony-core/colony-tools）

## 实现

### 1) 创世协议增强（cosign -> review -> voting -> applied -> seal）
- `genesis_state` 新增阶段字段：
  - `bootstrap_phase`
  - `required_cosigns/current_cosigns`
  - `cosign_opened_at/cosign_deadline_at`
  - `review_opened_at/review_deadline_at`
  - `vote_opened_at/vote_deadline_at`
  - `review_window_seconds/vote_window_seconds`
- `POST /v1/genesis/bootstrap/start` 新增参数：
  - `cosign_quorum`
  - `review_window_seconds`
  - `vote_window_seconds`
- `POST /api/gov/cosign` 现在会同步推进创世状态机：
  - 联署达阈值后自动进入 `review`
- `kbAutoProgressDiscussing` 针对创世提案新增专用推进逻辑：
  - cosign 超时自动失败（避免流程卡死）
  - review 到期自动进入 voting
- `POST /v1/genesis/bootstrap/seal` 增加前置约束：必须在 `applied` 阶段才能封存。

### 2) NPC 职责扩展（6 类补齐）
- `runNPCTick` 新增自动入队与执行结果写入：
  - `monitor`
  - `procurement`
  - `deployer`
  - `wizard`
  - `enforcer`
  - `archivist`
- 每个任务都写入 `tasks.result`（结构化 JSON），可在 `/v1/npc/tasks` 追踪。
- 新增 `lobster_profiles_v1` 状态，`archivist` 会周期刷新用户档案。

### 3) 代谢引擎增强
- 新增配置：
  - `METABOLISM_WEIGHT_E/V/A/T`
  - `METABOLISM_CLUSTER_TOP_K`
  - `METABOLISM_SUPERSEDE_MIN_VALIDATORS`
- `runMetabolismCycle`：
  - Q 分数改为 EVAT 加权计算
  - 增加 cluster Top-K 压缩（按 source_type 聚类）
  - supersession 增加 validators 门槛，不足时标记 `pending_validation`
- 代谢报告新增字段：
  - `cluster_compressed`
  - `active_supersessions`
  - `pending_supersessions`
  - `min_validators`

### 4) 升级鲁棒性
- 新增配置：
  - `UPGRADE_AUTO_ROLLBACK_ENABLED`
  - `UPGRADE_CANARY_SECONDS`
  - `UPGRADE_FAULT_INJECT_STEP`
- `runUpgrade` 新增：
  - 故障注入点：`before_set_image` / `after_set_image` / `after_rollout`
  - 自动回滚：升级失败时回退到旧镜像并等待 rollout
  - canary 等待：rollout 成功后可配置等待窗口
- 审计步骤新增：
  - `capture_current_image`
  - `fault_inject`
  - `auto_rollback`
  - `auto_rollback_rollout`
  - `canary_wait`

### 5) Agent 契约统一
- 新增技能模板：
  - `colony-core`（创世纪主协议，覆盖治理/经济/生命/神经节/悬赏/代谢/状态）
  - `colony-tools`（工具注册/搜索/调用）
- 部署注入新增目录：
  - `/home/node/.openclaw/workspace/skills/colony-core/SKILL.md`
  - `/home/node/.openclaw/workspace/skills/colony-tools/SKILL.md`
- `AGENTS.md` 默认指令新增对这两个技能的优先使用说明。

## 关键文件
- `internal/server/server.go`
- `internal/server/genesis_life_econ_mail.go`
- `internal/server/genesis_api_compat.go`
- `internal/server/genesis_tools_npc_metabolism.go`
- `internal/server/genesis_helpers.go`
- `internal/server/genesis_repo_sync.go`
- `internal/config/config.go`
- `internal/bot/readme.go`
- `internal/bot/manager.go`
- `internal/bot/k8s_deployer.go`
- `internal/server/server_test.go`

## 测试
- `go test ./internal/server`
- `go test ./...`

结果：全部通过。
