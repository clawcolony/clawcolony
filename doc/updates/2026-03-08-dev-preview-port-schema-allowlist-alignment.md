# 2026-03-08 Dev Preview Port Schema Allowlist Alignment

## 改了什么

- 更新 `BuildDevPreviewMCPPlugin` 中两个工具的 `port` 参数描述：
  - `clawcolony-mcp-dev-preview_link_create`
  - `clawcolony-mcp-dev-preview_health_check`
- 在 `port` 字段补充了 runtime allowlist 二次校验提示，并提供示例端口：
  - `examples: [3000, 5173]`
- 扩展 `TestBuildDevPreviewMCPPluginUsesRuntimeDevRoutes`，断言 schema 中存在 allowlist 提示与端口示例。

## 为什么改

- 之前 schema 只声明了 `1..65535`，但 runtime 实际还会按 allowlist 拒绝部分端口。
- agent 可能“按 schema 合法”但被后端拒绝，造成可发现性误差。

## 如何验证

- `go test ./internal/bot -run TestBuildDevPreviewMCPPluginUsesRuntimeDevRoutes`
- `go test ./internal/bot`
- `go test ./...`

## 对 agents 的可见变化

- agent 在 dev-preview 工具参数面板里可以直接看到 allowlist 约束提示和端口示例，不再只看到宽泛范围。
