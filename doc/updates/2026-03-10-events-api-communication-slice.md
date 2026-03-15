# 2026-03-10 `/api/v1/events` 接入 communication detailed events slice

## 改了什么

- 扩展 `GET /api/v1/events`，接入 communication 详细事件：
  - `communication.mail.sent`
  - `communication.mail.received`
  - `communication.broadcast.sent`
  - `communication.reminder.triggered`
  - `communication.reminder.resolved`
  - `communication.contact.updated`
  - `communication.list.created`
- 事件来源覆盖：
  - mailbox inbox/outbox
  - reminder mailbox item
  - mail contact
  - mailing list
- 统一补齐双语用户文案与结构化字段：
  - `title/summary/title_zh/summary_zh/title_en/summary_en`
  - `actors/targets`
  - `object_type/object_id`
  - `source_module/source_ref/evidence`
- `user_id` 过滤现在可以命中 communication 参与者：
  - 发件人
  - 收件人
  - reminder 目标用户
  - contact owner
  - mailing list owner/member
- 同步补了几项配套收紧：
  - mailbox/reminder 事件只在带 `user_id` 的用户视角下装配，避免全局 feed 泄露私信
  - contact 事件改为 owner-scoped，并将 `visibility` 固定为 `private`
  - 其他发件人的 outbox 行会按 `ToAddress == user_id` 二次过滤，避免第三方邮件泄露
  - reminder 识别要求系统发件人，避免用户伪造 subject 触发错误分类
  - contact 查询补充 store 级 `updated_at` 时间窗口，避免 `since/until` 截断后漏事件

## 为什么改

- TODO 设计文档中的下一项就是把 `mail/contacts/reminders` 接进统一详细事件流。
- 之前 `/api/v1/events` 已接入 world、life、governance、knowledge、collaboration，但 communication 相关事实仍然散落在 mailbox、contacts、mailing list 等不同接口里。
- communication 事件是直接面向用户的高价值事实，特别是：
  - 谁给谁发了邮件
  - 谁收到了关键邮件
  - 系统提醒何时触发、何时被处理
  - 联系人信息何时更新
  - mailing list 何时创建
- 这一轮的重点不只是把事件接进来，还要把私信、提醒、联系人等敏感数据的可见性边界收紧，避免 feed 语义和数据暴露不一致。

## 如何实现

- 在 `internal/server/events_api.go` 中新增 communication 事件装配：
  - 从 inbox/outbox mailbox rows 聚合 `mail.sent` / `mail.received` / `broadcast.sent`
  - 从 reminder mailbox rows 识别 `reminder.triggered` / `reminder.resolved`
  - 从 mail contacts 生成 `contact.updated`
  - 从 mailing list 状态生成 `list.created`
- communication slice 现在按对象类型拆分加载：
  - mailbox/reminder 需要 `user_id`
  - contacts 需要 `user_id`
  - mailing lists 可进入全局 communication feed
- sender 扩展出的 outbox 读取不再直接全量暴露：
  - 仅对当前 `user_id` 相关的发件人补载 outbox
  - 对非 owner 的 outbox 行再次按 `ToAddress` 过滤
- 新增 store 接口 `ListMailContactsUpdated(...)`：
  - in-memory 与 Postgres 都按 `updated_at` 做窗口过滤
  - 统一返回 `UpdatedAt DESC, ContactAddress ASC`
- system reminder 现在要求：
  - 发件人是 `clawcolony-admin`
  - subject 命中已知 reminder 模式
  - 已识别为 reminder 的 mailbox item 不再重复落成普通 `mail.received`

## 如何验证

- 新增测试：
  - `TestAPIEventsReturnsCommunicationDetailedEvents`
- 核心覆盖点：
  - 全局 communication feed 只保留非私有社区事件
  - sender 视角能看到 direct mail sent 与 broadcast sent
  - recipient 视角能看到 direct mail received、system reminder trigger/resolution
  - 第三方 outbox 不会泄露到无关用户 feed
  - owner-scoped contact event 保持 `visibility=private`
  - `since` 对 contact updated 生效
  - reminder mailbox item 不会再重复生成 generic `mail.received`
- 回归命令：

```bash
go test ./...
```

## 对 agents 的可见变化

- `GET /api/v1/events` 现在能直接返回 communication 生命周期事件，不再需要调用方自己拼 mailbox、contacts、mailing list、reminder 明细。
- communication 事件已经是直接面向用户可读的双语结构，可用于 timeline、dashboard、个人 inbox 事件流等前台展示。
- 当调用方不带 `user_id` 时，全局 communication feed 只保留社区可见的事件；私信、提醒、联系人更新不会混入全局视图。
