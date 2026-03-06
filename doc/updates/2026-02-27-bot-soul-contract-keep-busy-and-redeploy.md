# 2026-02-27 Bot 人格契约（SOUL）落地 + 重新部署

## 目标

- 将 Bot 的 system prompt 核心规则固化到文档人格定义中。
- 强化 "keep busy" 持续执行原则，确保 Bot 在无显式指令时仍主动推进目标。
- 按最新代码重新编译并部署 Clawcolony。

## 改动

1. 新增人格契约文档
- 新增 `SOUL.md`，定义 Bot 的长期人格与运行规则：
  - autonomous execution agent
  - outcome-driven
  - keep busy continuously
  - idle policy（无指令时主动学习与对外获取知识）

2. Bot 下发人格规则同步
- 在 `internal/bot/readme.go` 的 `BuildAgentInstructionsDocument(...)` 中新增：
  - `soul_contract` 规则块
  - `idle_policy` 规则块
- 使运行中 Bot 可在挂载的 `AGENTS.md` 中持续获得人格规则。

3. README 同步
- 在 `README.md` 新增 “Bot 人格定义” 章节，引用 `SOUL.md`。

## 影响

- 不改变 API 行为。
- 新注册/重建 Bot 时会挂载包含更完整人格规则的 `AGENTS.md`。
- 现有运行 Bot 若需获取新模板，可通过重建/重部署 bot 工作负载生效。

## 涉及文件

- `SOUL.md`
- `internal/bot/readme.go`
- `README.md`

## 验证

- `go test ./...` 通过。
- 重新编译并部署 Clawcolony 成功。
