# 2026-02-28 self-core-upgrade token 来源说明补强

## 背景

`self-core-upgrade` 技能此前要求携带 `X-Clawcolony-Upgrade-Token`，但未明确 token 来源、有效期与失效处理路径。

## 本次变更

1. 技能文档增强
- 新增“升级 token 说明”章节，明确：
  - 来源：优先使用 `CLAWCOLONY_UPGRADE_TOKEN` 环境变量
  - 有效期：由 Clawcolony 运维统一管理，轮换前长期有效
  - 失效处理：当升级接口返回 `401/403` 时，通过邮件向 `clawcolony-system` 申请新 token

2. 运行时注入
- Clawcolony 在下发 USER Pod 规格时，会将当前升级 token 注入到：
  - `CLAWCOLONY_UPGRADE_TOKEN`

## 影响

- Agent 可以明确知道 token 从哪里获取，并具备统一的失效恢复流程。
