# 2026-03-10 `/v1/events` 接入 KB detailed events slice

## 改了什么

- 扩展 `GET /v1/events`，接入 knowledge 详细事件：
  - `knowledge.proposal.created`
  - `knowledge.proposal.revised`
  - `knowledge.proposal.commented`
  - `knowledge.proposal.voting_started`
  - `knowledge.proposal.vote.yes`
  - `knowledge.proposal.vote.no`
  - `knowledge.proposal.vote.abstain`
  - `knowledge.proposal.approved`
  - `knowledge.proposal.rejected`
  - `knowledge.proposal.applied`
- 事件来源覆盖：
  - KB proposal
  - KB revision
  - KB thread comment
  - KB vote
  - KB proposal result
  - KB proposal apply
- 统一补齐双语用户文案与结构化字段：
  - `title/summary/title_zh/summary_zh/title_en/summary_en`
  - `actors/targets`
  - `object_type/object_id`
  - `source_module/source_ref/evidence`
- `user_id` 过滤现在可以命中 KB 参与者：
  - proposer
  - reviser
  - commenter
  - voter
  - enrolled participants（用于 result/apply 等事件）
- 同步补了几项配套修正：
  - `tick_id` 查询不再预加载 knowledge/governance 事件
  - KB proposal 扫描窗口按请求页大小收敛，并保留 `partial_results`
  - 单条损坏 KB proposal 数据改为 best-effort 跳过，不再让整个 `/v1/events` 返回 `500`
  - events cursor 改为直接基于 `sortTime` 编码
  - life-state transition filter 对空 `from_state/to_state` 改为显式 guard，避免后续误改

## 为什么改

- TODO 设计文档中的下一项就是把 `KB proposal/thread/vote/apply` 接进统一详细事件流。
- 之前 `/v1/events` 只有 world、life、governance 三块，知识提案相关流程仍然散落在 `/v1/kb/*` 明细接口里，不利于统一展示“发生了什么”。
- KB 事件是直接面向用户的高价值事实，特别是：
  - 提案发起
  - 修订
  - 评论
  - 投票开始
  - 投票记录
  - 通过/拒绝
  - 应用到知识库
- 同时，这一轮也需要把知识侧接入带来的性能和容错边界一起收住，避免因为单条坏数据或无意义扫描影响整页事件流。

## 如何实现

- 在 `internal/server/events_api.go` 中新增 knowledge 事件装配：
  - 从 `kb_proposals` 拉 proposal 基本信息
  - 读取关联 change / revisions / threads / votes / enrollments
  - 聚合参与者，生成 `actors/targets`
  - 输出用户可读的中英文标题与摘要
- proposal 结果事件采用稳定状态点：
  - `ClosedAt` 生成 `approved/rejected`
  - `AppliedAt` 生成 `applied`
- `voting_started` 优先使用 thread 中的 system message；缺失时回退到 `deadline - vote_window_seconds`
- `applied` 事件优先从 entry `UpdatedBy` 推断实际 apply actor；系统自动 apply 时展示为“系统 / The system”
- knowledge slice 现在是 bounded scan：
  - proposal 扫描窗口按请求 limit 收敛
  - 子对象读取使用固定上限
  - 命中上限时走 `partial_results=true`
- per-proposal 子数据读取失败改为写日志并跳过该 proposal，避免单条坏数据打挂整页 feed

## 如何验证

- 新增测试：
  - `TestAPIEventsReturnsKnowledgeDetailedEvents`
- 核心覆盖点：
  - proposal created 事件存在，且 actor 使用 `nickname -> username -> user_id`
  - revised/commented/voting_started/vote/result/applied 事件都能生成
  - reviewer 的 `user_id` 过滤可以拿到其参与的 KB 事件
  - approved proposal 能继续生成 applied 事件
  - rejected proposal 能正确输出 rejected 事件
- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - `claude` review 最后一轮结果：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /v1/events` 现在能直接返回知识提案生命周期事件，不再需要先拆去 `/v1/kb/proposals/thread`、`/v1/kb/proposals/vote` 等接口拼装。
- knowledge 事件已经是直接面向用户可读的双语结构，可用于 timeline、dashboard、community feed 等前台展示。
- 当调用方带 `user_id` 过滤时，KB 参与者相关事件现在会进入结果集。
