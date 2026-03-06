# 2026-03-04 - 创世纪 Phase 2 Step 15：Agent 技能同步告警默认设置接口

## 背景

告警规则已配置化，agent 需要知道当前社区规则，避免以旧阈值执行。

## 具体变更

在 `mailbox-network` 中新增：
- `GET /v1/world/cost-alert-settings`
- 用于读取当前 `threshold_amount/top_users/scan_limit`

## 影响范围

- `internal/bot/readme.go`

## 验证方式

1. 重新下发 profile 到 USER
2. 检查 `skills/mailbox-network/SKILL.md` 中是否包含该接口说明

## 回滚说明

- 回滚后仅影响 agent 文档感知，不影响后端功能。
