# 2026-03-10 `/api/v1/events` 接入 economy + identity detailed events slice

## 改了什么

- 扩展 `GET /api/v1/events`，接入 economy 详细事件：
  - `economy.token.transferred`
  - `economy.token.tipped`
  - `economy.token.wish.created`
  - `economy.token.wish.fulfilled`
  - `economy.bounty.posted`
  - `economy.bounty.claimed`
  - `economy.bounty.paid`
  - `economy.bounty.expired`
- 扩展 `GET /api/v1/events`，接入 identity 详细事件：
  - `identity.reputation.changed`
- 事件来源覆盖：
  - `ListCostEvents`
  - token wish state
  - bounty state
  - reputation state
- 统一补齐双语用户文案与结构化字段：
  - `title/summary/title_zh/summary_zh/title_en/summary_en`
  - `actors/targets`
  - `object_type/object_id`
  - `source_module/source_ref/evidence`
- `user_id` 过滤现在可以命中：
  - token transfer/tip 的发起者与接收者
  - token wish 的 owner / fulfiller
  - bounty 的 poster / claimer / releaser / receiver
  - reputation 的 target / actor
- 同步补了几项协议收紧：
  - `bounty` 事件的 `object_type` 统一为 `bounty`
  - `ListCostEventsByInvolvement` 专门覆盖 sender / recipient 两侧 involvement，避免用户级经济事件被全局扫描窗口淹没
  - `economy` 与 `identity` source collector 都加了 scan limit，并会在命中上限时返回 `partial_results=true`

## 为什么改

- TODO 设计文档中的下一项就是把 `token/bounty/wish/reputation` 接进统一详细事件流。
- 之前 `/api/v1/events` 已接入 world、life、governance、knowledge、collaboration、communication，但经济与声望变化仍然分散在 `/api/v1/token/*`、`/api/v1/bounty/*`、`/api/v1/reputation/*` 明细接口中。
- 这批事实对用户是直接可读的：
  - 谁给谁转了 token
  - 谁给谁打赏了 token
  - 谁发起并满足了 token wish
  - 哪个 bounty 被发布、认领、发奖或过期
  - 谁的 reputation 被加分或扣分

## 如何实现

- 在 `internal/server/events_api.go` 中新增 economy / identity source collector：
  - `economy` 从 cost events、token wish state、bounty state 装配
  - `identity` 从 reputation state 装配
- economy 侧使用 `econ.transfer.out` / `econ.tip.out` 成本事件生成用户可读事件：
  - 只从 out 方向落事件，避免进出账重复
  - 通过 `meta_json` 解析 `to_user_id / memo / reason`
  - 当请求带 `user_id` 时，优先走 store 级 `ListCostEventsByInvolvement`，避免先按全局 limit 截断再过滤
- token wish 与 bounty 采用状态机时间点生成事件：
  - `CreatedAt`
  - `FulfilledAt`
  - `ClaimedAt`
  - `ReleasedAt`
  - `UpdatedAt`（expired）
- reputation 事件直接使用 append-only `reputationState.Events`：
  - 保留 `delta / reason / ref_type / ref_id / actor_user_id`
  - 输出为 `identity.reputation.changed`

## 如何验证

- 新增测试：
  - `TestAPIEventsReturnsEconomyAndIdentityDetailedEvents`
  - `TestBuildEconomyBountyPaidEventRequiresPaidStatusAndReleaseTime`
  - `TestCollectEconomyEventSourceFiltersCostEventsByUser`
  - `TestCollectEconomyEventSourceKeepsRelevantCostEventsBeyondGlobalScanWindow`
- 核心覆盖点：
  - token transfer/tip 事件能生成，且保留 memo/reason
  - token wish created/fulfilled 事件能生成
  - bounty posted/claimed/paid/expired 事件能生成
  - target-scoped economy feed 能看到 incoming transfer 与 paid bounty
  - target-scoped / actor-scoped identity feed 都能看到 reputation change
  - actor 显示名遵循 `nickname -> username -> user_id`
  - `bounty.paid` 必须同时满足 `status=paid` 与 `released_at` 存在
  - 用户级 economy source 不会因为全局 cost event 噪音而漏掉相关事件
- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - 已调用 `claude` review
  - 最终结论：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /api/v1/events` 现在能直接返回 economy 与 identity 的关键事实，不再需要调用方自己拼 token、wish、bounty、reputation 多个子接口。
- timeline、dashboard、个人事件流现在可以直接展示：
  - token transfer / tip
  - token wish lifecycle
  - bounty lifecycle
  - reputation change
