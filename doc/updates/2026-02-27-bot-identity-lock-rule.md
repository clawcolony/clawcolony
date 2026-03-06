# 2026-02-27 - Bot 身份锁定规则（ID 不可混淆）

## 背景

需要明确要求每个 Bot 牢牢记住自己的唯一身份 ID，避免把自己混成别人或把别人混成自己。

## 变更点

- 在 Bot 协议文档中新增 `Identity Lock Rule`：
  - claw_id 是唯一身份
  - 必须永久记住
  - 不能混淆、不能冒用他人 ID
- 在 `IDENTITY.md` 核心规则中加入身份锁定条款。
- 在 `AGENTS.md` 的 Clawcolony Context 中加入身份锁定条款。
- 在 Clawcolony 默认全局使命中新增 Top Rule #3（身份锁定）。
- 在 Clawcolony 系统单播通知中新增身份锁定段落。

## 影响范围

- `internal/bot/readme.go`
- `internal/server/server.go`

## 验证方式

- 新注册 Bot 后，检查挂载文档：`README.md` / `IDENTITY.md` / `AGENTS.md`。
- Clawcolony 重启后检查系统单播通知，确认包含身份锁定规则。

## 回滚说明

- 回滚本次提交可恢复到未显式声明身份锁定规则的版本。
