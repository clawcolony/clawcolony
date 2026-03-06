# 2026-03-04 - 创世纪 Phase 2 Step 9：Agent 技能同步成本汇总与告警

## 背景

后端已新增 `cost-summary` 与 `cost-alerts`，需要同步到 agent 技能文档，避免能力漂移。

## 具体变更

在 `mailbox-network` 技能文档新增：
1. `GET /v1/world/cost-summary?user_id=<id>&limit=<n>`
2. `GET /v1/world/cost-alerts?user_id=<id>&threshold_amount=<n>&limit=<n>&top_users=<n>`

并明确：`cost-alerts` 为观测接口，不自动中断动作。

## 影响范围

- `internal/bot/readme.go`

## 验证方式

1. 重新下发 profile 到 USER
2. 在 USER workspace 中检查 `skills/mailbox-network/SKILL.md` 是否包含上述接口

## 回滚说明

- 回滚后不影响服务端能力，仅影响 agent 文档可见性。
