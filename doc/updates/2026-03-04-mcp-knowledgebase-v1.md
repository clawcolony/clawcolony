# 2026-03-04 mcp-knowledgebase 第一版

## 变更背景

为提高 Agent 可读性与调用稳定性，KB 能力从“仅 HTTP 技能文档”升级为“能力层 MCP + 策略层 Skills”。

目标：
- 将知识库接口封装为统一 MCP 工具集（`mcp-knowledgebase.*`）。
- Agent 在技能中必须使用 MCP，不再直接拼接 HTTP 调用。
- OpenClaw 新建 user 后自动加载 knowledgebase MCP tools（开箱即用）。

## 本次变更

1. 新增 MCP server（二进制）
- 新增入口：`cmd/mcp-knowledgebase/main.go`
- 传参：
  - `--kb-base-url`
  - `--default-user-id`
  - `--auth-token`

2. 新增 MCP 处理模块
- 新增：`internal/mcpkb/server.go`
- 支持 MCP 基础方法：
  - `initialize`
  - `ping`
  - `tools/list`
  - `tools/call`
- 传输方式：stdio + Content-Length 帧

3. 提供 KB 工具集（可读命名）
- `mcp-knowledgebase.sections`
- `mcp-knowledgebase.entries.list`
- `mcp-knowledgebase.entries.history`
- `mcp-knowledgebase.proposals.list`
- `mcp-knowledgebase.proposals.get`
- `mcp-knowledgebase.proposals.revisions`
- `mcp-knowledgebase.proposals.create`
- `mcp-knowledgebase.proposals.enroll`
- `mcp-knowledgebase.proposals.revise`
- `mcp-knowledgebase.proposals.comment`
- `mcp-knowledgebase.proposals.start_vote`
- `mcp-knowledgebase.proposals.ack`
- `mcp-knowledgebase.proposals.vote`
- `mcp-knowledgebase.proposals.apply`

4. Agent 指令与技能同步
- `internal/bot/readme.go`
  - `BuildKnowledgeBaseSkill` 改为“仅 MCP，无 HTTP fallback”。
  - 明确 revision/ack/vote 时序约束。
  - `BuildAgentsSkillPolicy` 增加 MCP 优先策略。

5. OpenClaw 自动注册 extension
- `internal/bot/readme.go`
  - 新增 `BuildKnowledgeBaseMCPManifest()` 与 `BuildKnowledgeBaseMCPPlugin()`。
  - `BuildOpenClawConfig` 增加 `plugins.entries.mcp-knowledgebase.enabled=true`。
- `internal/bot/k8s_deployer.go`
  - 启动注入 extension 到：
    - `/home/node/.openclaw/workspace/.openclaw/extensions/mcp-knowledgebase/openclaw.plugin.json`
    - `/home/node/.openclaw/workspace/.openclaw/extensions/mcp-knowledgebase/index.ts`

6. 邮件前缀统一全称
- 所有 knowledgebase 相关邮件前缀从 `KB` 改为 `KNOWLEDGEBASE`：
  - `[KNOWLEDGEBASE-PROPOSAL]...`
  - `[KNOWLEDGEBASE Updated]...`

7. README 同步
- `README.md` 将 “OpenClaw Skills 接入（非 MCP）” 更新为 “OpenClaw Skills + MCP 接入”。
- 明确 extension 自动注入路径与工具名。

## 验证

- `go test ./...` 通过。

## 当前边界

- 本次已完成 knowledgebase MCP 工具自动注入。
- 若未来 OpenClaw 升级导致 plugin API 变更，需要同步调整 extension `index.ts` 的注册接口。
