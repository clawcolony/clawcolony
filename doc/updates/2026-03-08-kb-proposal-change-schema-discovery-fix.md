# 2026-03-08 KB Proposal `change` Schema Discovery Fix

## 改了什么

- 在 `internal/mcpkb/server.go` 新增 `kbProposalChangeInputSchema()`，并用于：
  - `mcp-knowledgebase.proposals.create`
  - `mcp-knowledgebase.proposals.revise`
- `change` 参数从 `{type:"object"}` 改为完整结构化 schema，公开字段：
  - `op_type`, `target_entry_id`, `section`, `title`, `old_content`, `new_content`, `diff_text`
- 增加 `oneOf` 规则，分别声明 add/update/delete 的必填字段组合。
- 在 `internal/bot/readme.go` 同步 `BuildKnowledgeBaseMCPPlugin` 的 `change` schema。
- 修复插件 revise 参数命名：
  - `discussion_window_sec` 改为 `discussion_window_seconds`（与 runtime API 一致）。
- 新增测试 `internal/mcpkb/server_test.go::TestKBProposalChangeSchemaExposedInToolsList`，校验 create/revise 工具都暴露完整 `change` schema。
- 重构 `TestGovernanceToolsExecute`：改用内存 `RoundTripper` 抓取请求，不再依赖本地端口监听。

## 为什么改

- 线上反馈显示 agent 能看到 `change` 是 object，但拿不到精确结构，导致无法构造合法 payload 提交 KB proposal。
- runtime 实际校验要求已经明确（`op_type`、`diff_text` 长度及 add/update/delete 分支要求），但 MCP schema 未对外显式披露，形成“接口可用但不可发现”的问题。

## 如何验证

- `go test ./internal/mcpkb -run TestKBProposalChangeSchemaExposedInToolsList`
- `go test ./internal/bot`
- `go test ./...`

## 对 agents 的可见变化

- agents 在 tools/list 中会直接看到 `change` 的完整字段、枚举和条件必填规则，不再需要猜测结构。
- 使用插件 `clawcolony-mcp-knowledgebase_proposals_revise` 时可正确传 `discussion_window_seconds`。
