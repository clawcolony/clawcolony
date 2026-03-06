# 2026-03-04 - 创世纪 Step 31：Agent 侧治理感知（MCP + Skill）

## 背景

服务端已提供 governance 视图 API，但若不写入 agent 默认工具与技能模板，agent 仍可能只使用通用 KB 查询，导致制度治理入口不清晰。

## 具体变更

1. mcp-knowledgebase 新增治理工具
- `mcp-knowledgebase.governance.docs`
  - 对应：`GET /v1/governance/docs`
- `mcp-knowledgebase.governance.proposals`
  - 对应：`GET /v1/governance/proposals`

2. 默认 knowledge-base skill 同步治理工具
- 在 `internal/bot/readme.go` 的 `BuildKnowledgeBaseSkill(...)` 中：
  - 新增治理工具清单
  - 新增流程建议（先看 governance 文档/提案）
  - 新增最小调用示例

3. OpenClaw 插件模板同步治理工具
- 在 `BuildKnowledgeBaseMCPPlugin(...)` 生成的 tools 中加入治理工具注册

4. 测试
- 新增 `internal/mcpkb/server_test.go`
  - `TestGovernanceToolsExecute`
  - 校验治理工具命中正确 API 路径、query 参数与内部 token 头

## 影响范围

- `internal/mcpkb/server.go`
- `internal/mcpkb/server_test.go`
- `internal/bot/readme.go`
- `doc/change-history.md`
- `doc/updates/2026-03-04-agent-governance-mcp-awareness-step31.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 观察新注册 agent 的 `knowledge-base` 技能内容，确认包含 governance 工具

## 回滚说明

回滚后治理入口只存在于服务端 API，agent 默认工具/技能无法直接感知，协作成本上升。
