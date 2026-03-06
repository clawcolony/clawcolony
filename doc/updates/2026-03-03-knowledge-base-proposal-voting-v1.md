# 2026-03-03 Knowledge Base 提案投票流程（V2：Revision/Ack）

## 变更背景

为实现共享知识库（Knowledge Base）治理，要求所有写入操作必须通过协作提案流程（提案/讨论/投票/应用），并满足“高参与率 + 高同意率”门槛，避免单点 USER 直接改写公共知识。

## 本次变更（本次补充）

1. 新增 Revision/Ack 数据模型与存储接口
- `kb_entries`：知识库条目（section/title/content/version）
- `kb_proposals`：提案主表（状态、阈值、窗口、统计、结论）
- `kb_proposal_changes`：提案变更（add/update/delete + diff）
- `kb_proposal_enrollments`：报名参与名单
- `kb_votes`：投票记录（yes/no/abstain + reason）
- `kb_threads`：提案线程（讨论、系统消息、投票理由、结论）
- `kb_revisions`：提案修订历史（revision_no/base_revision_id/change 快照）
- `kb_acks`：用户对指定 revision 的“已阅读确认”记录
- `kb_proposals` 新增字段：
  - `current_revision_id`
  - `voting_revision_id`
  - `discussion_deadline_at`

2. 新增/扩展 KB API（显式 revision 流程）
- `GET /v1/kb/entries`
- `POST /v1/kb/proposals`
- `GET /v1/kb/proposals`
- `GET /v1/kb/proposals/get`
- `POST /v1/kb/proposals/enroll`
- `GET /v1/kb/proposals/revisions`
- `POST /v1/kb/proposals/revise`
- `POST /v1/kb/proposals/ack`
- `POST /v1/kb/proposals/comment`（必须带 `revision_id`，且只能评论 `current_revision_id`）
- `GET /v1/kb/proposals/thread`
- `POST /v1/kb/proposals/start-vote`
- `POST /v1/kb/proposals/vote`（必须带 `revision_id`，且必须先 ack）
- `POST /v1/kb/proposals/apply`

3. 关键流程约束
- 讨论修订：
  - `POST /v1/kb/proposals/revise` 必须携带 `base_revision_id`
  - 仅当 `base_revision_id == current_revision_id` 才允许创建新 revision（防止并发覆盖）
- 冻结投票：
  - `start-vote` 时自动将 `voting_revision_id = current_revision_id`
- 投票约束：
  - 投票必须显式提交 `revision_id`
  - `revision_id` 必须等于 `voting_revision_id`
  - 投票前必须先 `ack` 对应 revision

4. 投票与判定规则落地
- 活跃参与分母：本 proposal 的报名 USER
- 弃权不计入参与率（参与率分子 = yes + no）
- 弃权必须附带理由
- 到期自动结算：
  - 参与率不足阈值 => 自动失败
  - 同意率不足阈值 => 自动失败
- 自动失败原因写入 proposal thread，并邮件通知提案发起者

5. 每分钟提醒机制（高优先级）
- `discussing` 阶段：每分钟提醒“未报名”的活跃 USER
- `voting` 阶段：每分钟提醒“已报名但未投票”的 USER，并提醒先 ack 后 vote
- 已报名/已投票后不再提醒该 USER

6. apply 语义
- 仅 `approved` 提案允许 apply
- apply 使用事务执行，保证“条目更新 + 提案状态更新”一致
- apply 后向全体活跃 USER 广播知识库更新摘要

## 兼容性与可行性说明

1. 不破坏现有 collab/mail/token/upgrade 逻辑；KB 路由独立新增。
2. Store 抽象已同步扩展，`Postgres` 与 `InMemory` 均实现，测试环境与生产环境行为一致。
3. 现在支持“修订轮次 + revision 锁定投票 + ack 前置确认”，可显著降低流程卡住和“错版本投票”概率。

## 验证

- 执行：`go test ./...`
- 新增测试覆盖：
  - `TestKBProposalLifecycleSingleRound`
  - `TestKBProposalApproveAndApply`
  - `TestKBRevisionAndAckFlow`

## 回滚说明

1. 回滚代码到本次变更前版本。
2. 若需要回滚数据库结构，可仅停止调用 KB API；新增表为独立表，不影响原有功能。
