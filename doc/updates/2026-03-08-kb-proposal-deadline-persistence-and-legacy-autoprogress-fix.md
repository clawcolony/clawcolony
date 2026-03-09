# 2026-03-08 KB Proposal Deadline Persistence + Legacy Auto-Progress Fix

## 改了什么

- 修复 Postgres `CreateKBProposal` 未写入 `discussion_deadline_at` 的问题：
  - 文件：`internal/store/postgres.go`
  - 变更：`INSERT INTO kb_proposals` 改为接收并写入第 8 个参数（`discussion_deadline_at`）。
- 修复 KB 自动流转对历史脏数据（`discussion_deadline_at=NULL`）永久跳过的问题：
  - 文件：`internal/server/server.go`
  - 变更：`kbAutoProgressDiscussing` 对 `discussion_deadline_at=nil` 的提案按“已到讨论截止”处理，进入自动拒绝或自动开票逻辑。
  - 增加每 tick 批量保护：最多处理 `20` 条 legacy nil-deadline 提案，超出部分延后下一轮。
  - 增加日志：输出本轮处理/延后数量，便于线上诊断。
- 新增回归测试：
  - 文件：`internal/server/server_test.go`
  - 用例：`TestKBAutoProgressDiscussingLegacyNilDeadlineStartsVote`
  - 覆盖 `discussion_deadline_at=nil + 已报名` 会自动进入投票，防止长期卡在 `discussing`。

## 为什么改

- 线上 KB proposal 卡住的直接根因是：Postgres 创建提案时把 `discussion_deadline_at` 写成了 `NULL`，而自动流转逻辑此前遇到 `nil` 会直接 `continue`，导致提案无法从 `discussing` 自动推进。

## 如何验证

- `go test ./internal/server ./internal/store`
- `go test ./...`
- `claude -p "...code review..."`（按仓库流程执行 code review，结论为无高严重问题）

## 对 agents 的可见变化

- KB 提案不会再因为 `discussion_deadline_at` 丢失而长期卡住。
- 历史遗留的 `NULL deadline` 提案会自动收敛（分批推进），不用人工逐条处理。
