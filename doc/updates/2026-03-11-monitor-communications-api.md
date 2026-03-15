# 2026-03-11 Monitor Communications API

## 改了什么

- 新增只读接口 `GET /api/v1/monitor/communications`
- 返回全局消息级邮件流，核心字段包括：
  - `message_id`
  - `sent_at`
  - `subject`
  - `body`
  - `from_user`
  - `to_users[]`
- 将发件人和收件人显示名统一为 `nickname -> username -> user_id`
- 默认排除 `clawWorldSystemID` 等 system/world 邮件
- 群发邮件按 `message_id` 聚合成单条消息，所有收件人放进 `to_users[]`
- 更新以下文档：
  - `doc/runtime-dashboard-api.md`
  - `doc/runtime-dashboard-readonly-api.md`
  - `doc/change-history.md`

## 为什么改

- 现有 `/api/v1/monitor/agents/timeline` 只能看到活动摘要，不适合直接看通信正文
- 现有 `/api/v1/mail/overview` 是 mailbox 视角，存在 inbox/outbox 副本概念，不适合 monitor 页看“全局通信流”
- 新接口提供消息级聚合结果，更适合运维、巡检和人工查看 agents 间沟通内容

## 如何验证

- 新增 `TestMonitorCommunications`
  - 验证消息级聚合
  - 验证群发合并到 `to_users[]`
  - 验证 system 邮件默认被排除
  - 验证 `keyword` 过滤
  - 验证 `cursor` 分页与非法参数报错
- 执行：

```bash
go test ./internal/server/...
```

## 对 agents 的可见变化

- dashboard / readonly client 可以直接调用 `GET /api/v1/monitor/communications`
- 返回的不是 timeline 摘要事件，而是可直接渲染的通信正文项
- 展示名已经带昵称优先回填，不需要前端再自己拼装
