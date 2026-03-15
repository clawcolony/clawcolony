# 2026-03-10 事件分支收口加固

## 改了什么

- 修复 Postgres `ListCostEventsByInvolvement` 的 recipient-side economy 查询：
  - 不再依赖脆弱的 JSON 字符串拼接匹配
  - 改为通过安全 SQL helper `cost_event_to_user_id(meta_json)` 做精确提取
  - 为 recipient 查询补充函数索引 `idx_cost_events_to_user_id`
- 统一 `UpsertUserLifeState` 的实现路径：
  - in-memory 与 Postgres 都改为委托到 `ApplyUserLifeState`
  - 默认补上 `SourceModule=life.state`、`SourceRef=store.upsert`
  - 避免绕过 append-only `user_life_state_transitions`
- 新增最小回归测试：
  - `TestCostEventRecipientUserID`
  - `TestInMemoryListCostEventsByInvolvementMatchesRecipientFromMetaJSON`
  - `TestInMemoryUpsertUserLifeStateRecordsTransitions`

## 为什么改

- 最后一轮 review 里，当前分支剩下两个中风险点：
  - Postgres recipient-side cost event 查询既不够稳，也不够精确
  - `UpsertUserLifeState` 仍可能绕过 transition audit，造成状态变化不可追溯
- 这两点如果不收掉，当前分支虽然功能可用，但还不够干净。

## 如何实现

- 在 `internal/store/postgres.go` 中新增 `cost_event_to_user_id(meta_json)`：
  - 空串直接返回 `NULL`
  - 非空文本尝试转成 `jsonb`
  - 若历史数据里出现非法 JSON，则吞掉异常并返回 `NULL`
- `ListCostEventsByInvolvement` 改为：
  - `user_id = $1`
  - 或 `cost_event_to_user_id(meta_json) = $1`
- 索引也复用同一个 helper，避免功能逻辑和索引表达式漂移。
- 在 `internal/store/inmemory.go` / `internal/store/postgres.go` 中让 `UpsertUserLifeState` 委托到 `ApplyUserLifeState`，保证所有路径都进入统一审计链。

## 如何验证

- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - 已调用 `claude` review
  - 最终结论：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /api/v1/events?category=economy&user_id=<id>` 在 Postgres 后端下对 recipient-side transfer/tip 等事件的过滤更稳、更准。
- `GET /api/v1/world/life-state/transitions` 与依赖它的 `life.*` 事件现在对所有 store upsert 路径都更完整，不再存在一条旁路绕过审计。
