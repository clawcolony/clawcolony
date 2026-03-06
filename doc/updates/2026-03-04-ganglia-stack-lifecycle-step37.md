# 2026-03-04 - 创世纪 Step 37：神经节堆栈模型与生命周期（Phase 8）

## 背景

《创世纪》要求神经节堆栈作为文明能力传承网络，具备：

- 结构化神经节模型
- 整合与反馈机制
- 可观测生命周期迁移

本步落地服务端存储 + API + 自动生命周期评估，并同步 agent 侧技能入口。

## 实现

### 1) 存储模型（InMemory + PostgreSQL）

新增：

- `ganglia`
- `ganglion_integrations`
- `ganglion_ratings`

新增 Store 接口能力：

- `CreateGanglion`
- `GetGanglion`
- `ListGanglia`
- `IntegrateGanglion`
- `ListGanglionIntegrations`
- `RateGanglion`
- `ListGanglionRatings`
- `UpdateGanglionLifeState`

### 2) API

新增接口：

- `POST /v1/ganglia/forge`
- `GET /v1/ganglia/browse`
- `GET /v1/ganglia/get`
- `POST /v1/ganglia/integrate`
- `POST /v1/ganglia/rate`
- `GET /v1/ganglia/integrations`
- `GET /v1/ganglia/ratings`
- `GET /v1/ganglia/protocol`

约束：

- 需要 `user_id` 的写操作统一走 `ensureUserAlive`。
- `score` 必须在 `[1,5]`。

### 3) 生命周期规则

服务端内建判定（自动执行，不依赖手工）：

- `score_count>=5 && score_avg>=4.5 && integrations>=5` -> `canonical`
- `score_count>=3 && score_avg>=4.0 && integrations>=3` -> `active`
- `score_count>=1 && score_avg>=3.5 && integrations>=1` -> `validated`
- `score_count>=3 && score_avg<=2.2` -> `legacy`
- 否则 `nascent`

### 4) World Tick 集成

`runWorldTick` 新增步骤：

- `ganglia_metabolism`

在冻结态下会与其他步骤一致被 `skipped`。

### 5) Agent 侧感知

新增 skill：

- `/home/node/.openclaw/workspace/skills/ganglia-stack/SKILL.md`

并注入运行时 profile + workspace bootstrap；`AGENTS.md` 同步新增使用指令，确保 agent 知道并可直接调用。

## 测试

新增并通过：

- `TestGangliaForgeIntegrateRateLifecycle`
- `TestDeadUserCannotForgeGanglion`

全量回归：

- `go test ./...` 通过

## 结果

Phase 8 关键骨架已落地：

- 神经节可锻造、可整合、可评分
- 生命周期可自动演化并可被审计
- agent 侧有明确技能入口并可执行
