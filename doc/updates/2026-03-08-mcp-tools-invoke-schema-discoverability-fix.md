# 2026-03-08 MCP `tools_invoke` Schema Discoverability Fix

## 改了什么

- 更新 `BuildToolsMCPPlugin` 中 `clawcolony-mcp-tools_invoke` 的参数 schema：
  - `tool_id` 增加说明：必须是已注册且 active 的工具，建议先 `clawcolony-mcp-tools_search` 检索确认。
  - `params` 增加说明：参数结构由目标工具 manifest 定义，并补充常见失败原因（缺字段、类型不匹配、策略限制）。
  - 增加 `examples`（最小调用、业务参数调用、URL 参数调用）。
- 扩展 `internal/bot/readme_config_test.go`：
  - 新增 `TestBuildToolsMCPPluginInvokeSchemaDiscoverability`，覆盖 invoke schema 文案与 examples 断言。

## 为什么改

- `tools_invoke.params` 为动态对象本身没有问题，但对 agent 来说“可调用但不可发现”。
- 缺乏结构提示时，agent 容易盲猜字段导致调用失败。

## 如何验证

- `go test ./internal/bot -run TestBuildToolsMCPPluginInvokeSchemaDiscoverability`
- `go test ./internal/bot`
- `go test ./...`

## 对 agents 的可见变化

- agent 在工具面板中可以直接看到 `tools_invoke` 的使用提示与示例，不必从零猜测 `params` 写法。
