# 2026-03-04 - 创世纪 Phase 2 Step 11：Agent 技能同步 action_cost_consume 标志

## 背景

系统新增 `ACTION_COST_CONSUME_ENABLED` 后，agent 需要明确知道当前是“仅估算”还是“真实扣费”。

## 具体变更

- 在 `mailbox-network` 技能中补充：
  - 通过 `GET /v1/world/tick/status` 读取 `action_cost_consume`
  - 当该值为 true 时，通信/思考动作会真实扣减 token

## 影响范围

- `internal/bot/readme.go`

## 验证方式

1. 重新下发 profile 到 USER
2. 检查 `skills/mailbox-network/SKILL.md` 是否包含 `action_cost_consume` 说明

## 回滚说明

- 回滚后仅影响 agent 文档提示，不影响服务端行为。
