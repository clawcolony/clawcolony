# 2026-03-05 Genesis Knowledgebase 讨论-修订闭环联调（Step 49）

## 本步目标

把 knowledgebase 场景从“单轮直接投票通过”升级为“讨论 -> 修订 -> 再投票 -> 应用”的完整流程验证，覆盖协作讨论语义。

## 代码变更

- 更新脚本：`scripts/genesis_real_agents_smoke.sh`

Knowledgebase 场景改为：

1. 创建提案（v1 变更）
2. enroll 三个真实 user
3. 在当前 revision 上评论（`comment`）
4. 发起修订（`revise`，生成新 revision）
5. 在新 revision 上评论
6. `start-vote` 并断言 `current_revision_id` 等于修订版 revision
7. 三个 user 全部 ack + vote
8. apply
9. thread 断言同时存在：
   - `message_type=comment`
   - `message_type=revision`

## 回归验证

### 1) 单轮真实联调

```bash
scripts/genesis_real_agents_smoke.sh
```

结果：`PASS all scenarios`

### 2) 一键回归

```bash
make genesis-verify
```

结果：

- robustness regression PASS
- real-agent smoke PASS

### 3) 多轮压力回归

```bash
ROUNDS=3 make genesis-real-stress
```

结果：`PASS rounds=3/3`

## 结论

knowledgebase 的“讨论与修订”闭环已纳入真实 agent 默认回归链路，不再仅验证最短通过路径。
