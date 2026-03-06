# 2026-03-04 - 创世纪 Step 32：治理流程自动闭环（讨论期自动推进）

## 背景

知识库治理中，提案可能长期停在 `discussing` 阶段，造成流程中断。需要引入自动推进机制，确保提案在讨论截止后进入确定结果路径。

## 具体变更

1. 提案创建支持讨论窗口
- `POST /v1/kb/proposals` 新增可选字段：
  - `discussion_window_seconds`
- 默认值：`300` 秒
- 限制：`<= 86400`
- 创建时写入 `discussion_deadline_at`

2. kbTick 自动推进 discussing 提案
- 新增 `kbAutoProgressDiscussing(...)`，并接入 `kbTick`
- 规则：
  - 到达 `discussion_deadline_at` 且 `enrollments=0`：
    - 自动关闭为 `rejected`
    - 记录线程 result
    - 邮件通知 proposer
  - 到达 `discussion_deadline_at` 且 `enrollments>0`：
    - 自动 `start voting`
    - 记录系统线程（auto start voting）
    - 给已报名 user 发送 `[ACTION:VOTE]` 置顶通知

3. MCP / Agent 模板同步
- `mcp-knowledgebase.proposals.create` 新增 `discussion_window_seconds` 参数
- 默认 knowledge-base skill 文本补充该参数建议

4. 测试
- 新增 `TestKBAutoProgressDiscussingNoEnrollmentRejects`
- 新增 `TestKBAutoProgressDiscussingStartsVote`
- 确保讨论期截止后提案不会卡死

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/mcpkb/server.go`
- `internal/bot/readme.go`
- `doc/change-history.md`
- `doc/updates/2026-03-04-governance-auto-progress-step32.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 创建带短讨论窗口（如 1s）的提案，等待后触发 `kbTick` 验证自动推进

## 回滚说明

回滚后提案仍可能长期停留在讨论阶段，治理流程存在中断风险。
