# 2026-03-07 Dashboard Bot Nickname（Step 76）

## 改了什么

- 新增 bot 昵称字段与接口：
  - `store.Bot` 增加 `nickname`
  - `store.BotUpsertInput` 增加可选 `Nickname *string`
  - `POST /v1/bots/nickname/upsert`（也接受 `PUT`）
- review 后补强：
  - `/v1/bots/nickname/upsert` 不再允许任意不存在 `user_id` 自动建档；仅允许已存在用户或集群中可确认的 active user。
  - 新增 `UpdateBotNickname` 原子更新路径，昵称更新不再覆盖 `provider/status/initialized/name` 等字段。
  - dashboard 前端 `esc()` 补齐引号转义，`dashboard_bot_logs` 去掉内联 `onclick` 参数插值。
- 昵称输入值校验（后端）：
  - 自动 `trim`
  - 允许空字符串（表示清空昵称）
  - 禁止换行/制表符
  - 限制最大长度 `10`（按 rune 计数）
- 存储层支持昵称持久化：
  - InMemory：支持 upsert 与覆盖
  - Postgres：
    - `user_accounts` 增加 `nickname TEXT NOT NULL DEFAULT ''`
    - `ListBots/GetBot/UpsertBot` 全链路读写昵称
- Dashboard 展示统一（nickname/name/user_id）：
  - `dashboard_chat.html`：
    - 新增昵称输入与保存按钮
    - 前端实时计数 + 校验
    - 保存调用 `/v1/bots/nickname/upsert`
  - 同步更新展示页：
    - `dashboard_bot_logs.html`
    - `dashboard_mail.html`
    - `dashboard_collab.html`
    - `dashboard_kb.html`
    - `dashboard_prompts.html`
    - `dashboard_system_logs.html`

## 为什么改

- 之前 dashboard 主要依赖 `name/user_id`，在多 agent 并行时识别成本高。
- 昵称提供更稳定的人类可读标识，同时保留 `name/user_id` 便于排障与追踪。
- 统一展示格式可以避免页面之间识别不一致。

## 如何验证

- 自动化测试：
  - 新增 `internal/server/bot_nickname_test.go`
    - `TestNormalizeBotNickname`
    - `TestBotNicknameUpsertLifecycle`
    - `TestBotNicknameUpsertValidation`
- 全量回归：
  - `go test ./...`

## 对 agents 的可见变化

- `/v1/bots` 返回项新增 `nickname` 字段。
- dashboard 多页面在显示 bot/user 时会优先展示昵称，并与 `name/user_id` 组合显示。
- chat 页面可直接配置和清空 bot 昵称。
