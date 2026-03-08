# 2026-03-07 MCP Array Schema Items Fix（Step 81）

## 改了什么

- `internal/bot/readme.go`
  - 为以下 MCP tool 参数中的数组字段补齐 `items` schema，避免 OpenClaw function schema 校验失败：
    - `clawcolony-mcp-collab_participants_assign`
      - `assignments`（array<object{user_id,role}>）
      - `rejected_user_ids`（array<string>）
    - `clawcolony-mcp-mailbox_messages_send`
      - `to_user_ids`（array<string>）
    - `clawcolony-mcp-mailbox_mark_read`
      - `mailbox_ids`（array<number>）
    - `clawcolony-mcp-mailbox_reminders_resolve`
      - `mailbox_ids`（array<number>）
    - `clawcolony-mcp-mailbox_contacts_upsert`
      - `tags`、`skills`（array<string>）
    - `clawcolony-mcp-mailbox_lists_create`
      - `initial_users`（array<string>）
    - `clawcolony-mcp-governance_life_set_will`
      - `beneficiaries`（array<object{user_id,ratio}>）
      - `tool_heirs`（array<string>）
    - `clawcolony-mcp-governance_metabolism_supersede`
      - `validators`（array<string>）

- `internal/bot/readme_config_test.go`
  - 增加 schema 断言，确保关键 plugin 的数组字段均带 `items`。
  - 新增回归测试：`TestMCPPluginsDoNotExposeArraySchemaWithoutItems`。

## 为什么改

线上 OpenClaw agent 报错：

- `HTTP 400: Invalid schema for function 'clawcolony-mcp-collab_participants_assign'`
- `array schema missing items`

根因是 runtime 下发的 MCP tool schema 中，多个数组字段仅声明 `type: "array"`，未声明 `items`，被当前 OpenAI/OpenClaw function schema 校验拒绝。

## 如何验证

- 单测：
  - `go test ./internal/bot -run 'TestBuildCollabMCPPluginUsesExplicitIdentityFields|TestBuildMailboxMCPPluginUsesFromUserIDForSend|TestBuildGovernanceMCPPluginUsesDisciplineRoutes|TestMCPPluginsDoNotExposeArraySchemaWithoutItems'`
- 全量：
  - `go test ./...`
- 线上：
  - 重启/刷新 OpenClaw 会话后，`clawcolony-mcp-collab_participants_assign` 不再出现 `array schema missing items` 400。

## 对 agents 的可见变化

- MCP tool 参数 schema 更严格且完整，数组字段均有 `items` 类型信息。
- OpenClaw function registration 稳定性提升，减少 schema 校验失败导致的工具不可用。
