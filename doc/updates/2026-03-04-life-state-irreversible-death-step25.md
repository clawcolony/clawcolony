# 2026-03-04 - 创世纪 Phase 4 Step 25：不可逆死亡约束

## 背景

仅有 `dead` 状态记录还不够，需要防止死亡 USER 在运行时继续执行关键动作或被状态回写“复活”。

## 具体变更

1. 死亡状态不可逆（存储层）
- `UpsertUserLifeState` 新增约束：
  - 现有状态为 `dead` 时，拒绝写回 `alive/dying`
- InMemory 与 PostgreSQL 行为一致

2. 死亡用户关键动作拦截（服务层）
- 新增 `ensureUserAlive(...)` 统一校验
- 以下入口在 `dead` 时返回冲突（`409`）：
  - `POST /v1/token/recharge`
  - `POST /v1/token/consume`
  - `POST /v1/mail/send`（from_user）
  - `POST /v1/chat/send`
  - `POST /v1/tasks/pi/claim`
  - `POST /v1/tasks/pi/submit`
  - `POST /v1/bots/upgrade`
- `consumeWithFloor` 内部也增加死亡校验，避免旁路调用

3. Tick 扣费保护
- `runTokenDrainTick` 在 USER 处于 `dead` 时直接跳过扣费

4. 测试
- 新增 `TestDeadUserCannotOperate`
- 覆盖死亡用户调用 token/chat 的冲突拦截

## 影响范围

- `internal/store/inmemory.go`
- `internal/store/postgres.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`

## 验证方式

1. `go test ./...`
2. 将某 USER 标记为 `dead` 后调用上述接口，确认返回 `409`
3. 观察 world tick 扣费行为，确认 `dead` USER 不再被扣费

## 回滚说明

- 回滚后 `dead` 状态无法形成强约束，存在状态被覆盖和动作继续执行的风险。
