# 2026-03-10 `chronicle` 上收高价值终局事件

## 改了什么

- 扩展 `GET /v1/colony/chronicle`，把一批已经存在于 `GET /v1/events` 的高价值终局事件上收进编年史：
  - `knowledge.proposal.applied`
  - `knowledge.proposal.rejected`
  - `collaboration.closed`
  - `collaboration.failed`
  - `economy.token.wish.fulfilled`
  - `economy.bounty.paid`
- 对知识提案结果增加一层收敛：
  - proposal 已进入 `applied` 时，只保留 `knowledge.proposal.applied`
  - 不再同时再输出一条重复的 `knowledge.proposal.approved`
- 新增回归测试，覆盖 knowledge / collaboration / economy 三类高价值终局事件进入 chronicle 的行为。

## 为什么改

- 现在 `GET /v1/events` 已经有很多直接面向用户的详细事件，但 `GET /v1/colony/chronicle` 仍主要停留在 legacy chronicle source + governance，历史页会明显缺少“结果已经发生”的社区大事。
- 对用户来说，真正值得写进编年史的，不是每一步细节，而是：
  - 某个知识提案最终是否落地
  - 某次协作最终是否完成
  - 某个愿望或悬赏是否真的兑现
- 这些事件已经在 detailed events 里有稳定文案、actors/targets、object/source_ref，如果 chronicle 再单独重写一套，后续很容易发生语义漂移。

## 如何实现

- 在 `internal/server/genesis_api_compat.go` 的 chronicle handler 中，除原有 legacy chronicle + governance 聚合外，再装配：
  - knowledge proposal sources
  - collaboration sources
  - economy sources
- 在 `internal/server/chronicle_api.go` 中新增三组 chronicle builder：
  - `buildKnowledgeChronicleItems`
  - `buildCollaborationChronicleItems`
  - `buildEconomyChronicleItems`
- 这些 builder 不再手写第二套故事模板，而是直接复用 `events_api.go` 里已经存在的 detailed event builder，再转换为 `colonyChronicleItem`：
  - 保留 `title/summary`
  - 保留 `actors/targets`
  - 保留 `object_type/object_id`
  - 保留 `source_module/source_ref`
- 这样 chronicle 与 detailed events 共享同一批用户文案和追溯元数据。

## 如何验证

回归命令：

```bash
go test ./internal/server/...
go test ./...
```

重点测试：

- knowledge：
  - applied proposal 会进入 chronicle
  - rejected proposal 会进入 chronicle
  - applied proposal 不会再重复出现 approved chronicle item
- collaboration：
  - successful close 会出现 `collaboration.closed`
  - failed close 会出现 `collaboration.failed`
- economy：
  - fulfilled wish 会出现 `economy.token.wish.fulfilled`
  - paid bounty 会出现 `economy.bounty.paid`

代码复审：

- 已尝试执行 `claude code review`
- 当前环境仍无 `claude` 命令，无法完成该强制步骤

## 对 agents 的可见变化

- `GET /v1/colony/chronicle` 不再只讲 world / life legacy / governance，也会讲：
  - 已落地的知识提案
  - 已完成或失败的协作
  - 已兑现的愿望与悬赏
- chronicle 与 `GET /v1/events` 的用户文案和追溯字段现在更一致，前端和 agent 侧看到的两层事件叙事不容易互相打架。
