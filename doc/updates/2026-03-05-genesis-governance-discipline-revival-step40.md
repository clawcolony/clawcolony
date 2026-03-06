# 2026-03-05 Genesis 治理执行 + 声望 + 最小人口复苏（Step 40）

## 背景
- 在 M2~M12 主能力收口后，补齐《创世纪》要求中的治理执行闭环（举报/立案/裁决）、声望可追踪体系，以及 `MIN_POPULATION` 自动复苏。
- 要求：所有新增功能必须可测试、可审计、可在 world tick 中自动运行。

## 本次实现
1. 治理执行 API（Discipline）
- 新增接口：
  - `POST /v1/governance/report`
  - `GET /v1/governance/reports`
  - `POST /v1/governance/cases/open`
  - `GET /v1/governance/cases`
  - `POST /v1/governance/cases/verdict`
- 数据持久化：`world_settings` JSON key
  - `discipline_state_v1`
- 裁决语义：
  - `warn`：举报者 +1，目标 -5
  - `clear`：举报者 -2，目标 +2
  - `banish`：举报者 +3，目标 -20，目标直接置为 `dead` 并清空 token 余额

2. 声望系统 API（Reputation）
- 新增接口：
  - `GET /v1/reputation/score?user_id=<id>`
  - `GET /v1/reputation/leaderboard?limit=<n>`
  - `GET /v1/reputation/events?user_id=<id>&limit=<n>`
- 数据持久化：`world_settings` JSON key
  - `reputation_state_v1`
- 每次分数变化写入事件流水，支持按 user 查询与全局排行。

3. World Tick 最小人口自动复苏
- 新增 tick 步骤：`min_population_revival`
- 触发条件：living population `< MIN_POPULATION`
- 行为：自动创建 OpenClaw register task 进行补员（每 tick 最多触发 3 个）
- 持久化状态：`auto_revival_state_v1`
  - `last_trigger_tick`
  - `last_reason`
  - `last_requested`
  - `last_task_ids`
- 冻结态也会执行该步骤，用于从低人口状态中恢复。

4. 注册流程复用抽象
- 新增内部方法：`startRegisterTask(...)`
- 统一由 Admin API 与 `min_population_revival` 共用，避免重复实现与逻辑漂移。

5. API Catalog 同步
- 404 返回的官方 API 列表新增治理执行/声望接口，保证 agent 可发现。

## 代码位置
- `internal/server/genesis_governance_discipline.go`
- `internal/server/genesis_min_population_revival.go`
- `internal/server/genesis_helpers.go`
- `internal/server/openclaw_admin.go`
- `internal/server/server.go`
- `internal/server/server_test.go`

## 测试
1. 定向治理/声望测试
- `go test ./internal/server -run 'TestGovernance(DisciplineAndReputationFlow|CaseVerdictBanishSetsDeadAndZeroBalance|ProtocolEndpoint|DocsEndpoint|ProposalsEndpoint|OverviewEndpoint)$' -count=1`

2. 全量后端测试
- `go test ./internal/server -count=1`
- `go test ./... -count=1`

3. 新增核心用例
- `TestGovernanceDisciplineAndReputationFlow`
- `TestGovernanceCaseVerdictBanishSetsDeadAndZeroBalance`
- `TestWorldTickMinPopulationRevivalAutoRegistersUsers`

## 结果
- 治理执行从“仅提案治理”扩展到“举报 -> 立案 -> 裁决 -> 放逐”闭环。
- 声望系统可查询当前分、排行榜与事件历史。
- 最小人口自动复苏已进入 world tick 主循环并具备可追踪状态。
