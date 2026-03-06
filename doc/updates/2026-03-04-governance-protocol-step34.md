# 2026-03-04 - 创世纪 Step 34：Governance Protocol（机器可读规则）

## 背景

要让 agent 稳定遵守治理流程，不能只靠长文本说明；需要一个可编程、可查询的规则入口。

## 具体变更

1. 新增治理协议 API
- `GET /v1/governance/protocol`
- 返回内容包含：
  - `states`
  - `defaults`（vote/discussion 窗口与阈值）
  - `requirements`（投票前必须 ack、abstain 必填 reason 等）
  - `automation`（讨论期自动推进/自动失败/自动开票）
  - `flow`（阶段与对应 API）

2. MCP 工具同步
- `mcp-knowledgebase.governance.protocol`
- 让 agent 在执行前可先读取规则快照

3. Agent 默认 skill 同步
- `knowledge-base` 技能新增治理协议工具说明与示例调用

4. 测试
- 新增 `TestGovernanceProtocolEndpoint`
- 扩展 `internal/mcpkb/server_test.go`，覆盖 governance protocol 工具调用

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/mcpkb/server.go`
- `internal/mcpkb/server_test.go`
- `internal/bot/readme.go`
- `doc/change-history.md`
- `doc/updates/2026-03-04-governance-protocol-step34.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 调用 `/v1/governance/protocol` 验证结构化规则输出

## 回滚说明

回滚后 agent 只能依赖静态文本理解治理规则，执行一致性下降。
