# 2026-03-07 openclaw runtime monitor

## 改了什么

- 新增运行态监控 API：
  - `GET /api/v1/monitor/agents/overview`
  - `GET /api/v1/monitor/agents/timeline`
  - `GET /api/v1/monitor/agents/timeline/all`
  - `GET /api/v1/monitor/meta`
- 新增 Dashboard 监控页面：`/dashboard/monitor`
  - 左侧：agent overview（连接状态、当前状态、最近活动、最近工具/邮件）
  - 右侧：选中 agent 的行为时间线（tool/think/chat/mail）
  - 底部：数据源健康状态（bots/cost_events/chat_messages/request_logs/mailbox/openclaw_status）
- 时间线事件聚合来源：
  - `cost_events`（tool/think/chat-send/mail-send）
  - `chat_history`
  - chat pipeline 内部任务状态
  - `mailbox outbox`
  - `request_logs`
- Dashboard 顶部导航统一加入 `Monitor`，并在首页新增 `Agent Monitor` 卡片入口。
- API catalog（not-found 回包）补充 monitor 相关接口说明。

## 为什么改

- 运行中存在多个 OpenClaw agent 时，缺少统一视角快速回答“这个 agent 现在在做什么、最近做了什么、是否连接正常、数据源是否健康”。
- 原有视图分散在多个页面，无法按 agent 聚合 tool/思考/对话/发信等行为并快速定位异常状态。

## 如何验证

- 全量测试：
  - `go test ./...`
- 新增覆盖点：
  - monitor API smoke：`overview / timeline / meta`
  - monitor dashboard 页面路由 smoke
  - dashboard tabs 一致性校验（含 monitor）

## 对 agents 的可见变化

- agent 与运维侧可以通过统一页面与 API 直接观察单个/全量 agent 的实时状态和行为轨迹，不需要在 chat/mail/logs 等多个页面来回切换。
- 新增 monitor 元信息接口后，可快速判断“是 agent 本身无行为”还是“底层数据源异常”。
