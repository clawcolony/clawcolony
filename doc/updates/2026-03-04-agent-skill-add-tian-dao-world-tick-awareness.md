# 2026-03-04 - Agent 技能补齐：天道与世界 Tick 感知

## 背景

创世纪要求 agent 对规则感知与服务端同源。此前 mailbox-network 主要覆盖 mail/knowledgebase 流程，缺少对天道快照与 world tick 的明确读取指引。

## 具体变更

在 `internal/bot/readme.go` 的 `mailbox-network` 技能中新增“创世纪规则感知（必读）”章节：

1. `GET /v1/tian-dao/law`
- 用于读取不可变天道参数与 hash。

2. `GET /v1/world/tick/status`
- 用于查看统一时钟运行状态。

3. `GET /v1/world/tick/history?limit=<n>`
- 用于排查最近 tick 历史异常。

## 影响范围

- 影响文件：`internal/bot/readme.go`
- 影响对象：新下发模板后的 OpenClaw agents。

## 验证方式

1. 重新下发模板后，在 agent workspace 中检查 `skills/mailbox-network/SKILL.md`。
2. 确认出现上述 3 个接口说明。

## 回滚说明

- 回滚本次技能文档变更即可。
