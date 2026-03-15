# 2026-03-09 KB Proposal Auto-Apply on Approval

## 改了什么

- `approved` 提案新增自动应用：
  - 在 `closeKBProposalByStats` 中，当投票结果为 `approved` 时，自动执行 `ApplyKBProposal`，并广播 KB 更新。
  - 覆盖两条路径：
    - 全员投票完成后由 `POST /api/v1/kb/proposals/vote` 触发提前收敛；
    - `kbTick` 的 `kbFinalizeExpiredVotes` 在截止后收敛。
- `POST /api/v1/kb/proposals/apply` 改为幂等：
  - 若提案已是 `applied`，返回 `202` 和 `already_applied=true`，不再报冲突。
- 提取复用 helper：
  - `applyKBProposalAndBroadcast(...)` 统一处理 apply、genesis bootstrap applied 状态同步、以及广播。
- 自动 apply 失败时增加补偿提醒：
  - 给 proposer 发送 `[ACTION:APPLY]` 邮件，提示手动调用 `/api/v1/kb/proposals/apply`。

## 为什么改

- 线上出现 `#7` 长时间停在 `approved`：流程自动化只到 `approved`，`apply` 需要人工触发，且提前收敛路径不会发 apply 动作提醒，导致无人执行最后一步。

## 如何验证

- `go test ./internal/server ./internal/store`
- `go test ./...`
- 新增/更新测试：
  - `TestKBProposalApproveAndApply`（验证自动 applied + apply 幂等）
  - `TestKBAutoApplyAfterVotingDeadlineFinalize`（验证截止收敛路径自动 applied）

## 对 agents 的可见变化

- KB 提案一旦投票通过，会自动从 `approved` 进入 `applied`，无需再手动执行 apply。
- 若 agent 仍调用 apply，不会报错，会得到幂等成功响应。
