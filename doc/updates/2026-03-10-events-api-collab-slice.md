# 2026-03-10 `/api/v1/events` 接入 collaboration detailed events slice

## 改了什么

- 扩展 `GET /api/v1/events`，接入 collaboration 详细事件：
  - `collaboration.created`
  - `collaboration.applied`
  - `collaboration.assigned`
  - `collaboration.accepted`
  - `collaboration.started`
  - `collaboration.progress.reported`
  - `collaboration.artifact.submitted`
  - `collaboration.review.approved`
  - `collaboration.review.rework_requested`
  - `collaboration.resubmitted`
  - `collaboration.closed`
  - `collaboration.failed`
- 事件来源覆盖：
  - collab session
  - collab participant
  - collab artifact
  - collab event
- 统一补齐双语用户文案与结构化字段：
  - `title/summary/title_zh/summary_zh/title_en/summary_en`
  - `actors/targets`
  - `object_type/object_id`
  - `source_module/source_ref/evidence`
- `user_id` 过滤现在可以命中协作参与者：
  - proposer
  - applicant
  - selected participant
  - artifact author
  - reviewer
- 同步补了几项配套修正：
  - collaboration session 扫描窗口按请求页大小收敛，并保留 `partial_results`
  - `object_type=collab_session&object_id=<id>` 走精确装配，不依赖全表扫描
  - 单条坏 payload 或单个协作子对象读取失败时改为 best-effort 跳过，不让整个 `/api/v1/events` 返回 `500`
  - `collaboration.created` 把全体参与者写入 `targets`，避免非 proposer 在 `user_id` 过滤下看不到协作发起事件
  - `collaboration.started` 改为解码正确的 executing payload，避免错误复用 close payload

## 为什么改

- TODO 设计文档中的下一项就是把 `collab events/artifacts` 接进统一详细事件流。
- 之前 `/api/v1/events` 已接入 world、life、governance、knowledge，但协作流程仍然散落在 `/api/v1/collab/*` 接口里，调用方必须自己拼装生命周期。
- 协作是直接面向用户的高价值事实，特别是：
  - 协作发起
  - 报名与录用
  - 正式开始
  - 进度汇报
  - 产物提交
  - review 通过或要求返工
  - 最终关闭或失败
- 这一轮也需要把协作侧接入后的扫描边界、容错和 `user_id` 可见性一起收紧，避免 feed 漏人或被坏数据打挂。

## 如何实现

- 在 `internal/server/events_api.go` 中新增 collaboration 事件装配：
  - 读取 collab session 基本信息
  - 读取 participants / artifacts / events
  - 聚合参与者，生成 `actors/targets`
  - 输出用户可读的中英文标题与摘要
- collaboration slice 现在是 bounded scan：
  - session 扫描窗口按请求 `limit` 收敛
  - 子对象读取使用固定上限
  - 命中上限时返回 `partial_results=true`
- exact-load 场景下，`object_type=collab_session&object_id=<id>` 会直接走单条 session 装配
- event type 到详细事件的映射：
  - `proposal.created -> collaboration.created`
  - `participant.applied -> collaboration.applied`
  - `participant.assigned -> collaboration.assigned / collaboration.accepted`
  - `collab.executing -> collaboration.started`
  - `artifact.submitted -> collaboration.progress.reported / collaboration.artifact.submitted / collaboration.resubmitted`
  - `artifact.reviewed -> collaboration.review.approved / collaboration.review.rework_requested`
  - `collab.closed -> collaboration.closed / collaboration.failed`
- per-session 子数据读取失败改为写日志并跳过该协作，避免单条坏数据打挂整页 feed

## 如何验证

- 新增测试：
  - `TestAPIEventsReturnsCollaborationDetailedEvents`
- 核心覆盖点：
  - `created/applied/assigned/accepted/started` 事件都能生成
  - artifact 提交能区分 `progress.reported`、`artifact.submitted`、`resubmitted`
  - review 能区分 `approved` 与 `rework_requested`
  - close 能区分 `closed` 与 `failed`
  - executor 的 `user_id` 过滤可以拿到其参与的协作事件，包括 `collaboration.created`
  - actor 显示名遵循 `nickname -> username -> user_id`
- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - `claude` review 最后一轮结果：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /api/v1/events` 现在能直接返回协作生命周期事件，不再需要先拆去 `/api/v1/collab/*` 多个接口拼装时间线。
- collaboration 事件已经是直接面向用户可读的双语结构，可用于 timeline、dashboard、community feed 等前台展示。
- 当调用方带 `user_id` 过滤时，协作参与者相关事件现在会进入结果集，而不是只命中 proposer。
