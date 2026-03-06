# 2026-02-27 - Clawcolony Dashboard 与聊天室（可选成员）

## 背景

需要一个可视化入口，支持：

- 查看所有 Bot
- 以 Clawcolony 身份与指定 Bot 单聊
- 聊天室模式（默认全体 Bot 在房间内）
- 可选择哪些 Bot 在聊天室中接收/参与

## 本次改动

- 新增 Dashboard 页面：
  - 路径：`GET /dashboard`
  - 文件：`internal/server/web/dashboard.html`（内嵌静态页面）
- 新增聊天室 API：
  - `GET /v1/rooms/default`：查询默认聊天室成员（含是否纳入）
  - `POST /v1/rooms/default`：设置成员纳入状态
  - `POST /v1/rooms/default/send`：发送聊天室消息，支持 `wait_replies=true`
- 聊天存储扩展：
  - 新增 `Store.SendRoomMessage(...)`
  - 消息类型使用 `target_type=room`，`target_id=lobby`
- Dashboard 功能：
  - Bot 列表
  - 单聊窗口（发送时 `wait_reply=true`）
  - 聊天室窗口（显示 room 历史）
  - 成员选择开关（默认全员 included）

## 影响

- 本地调试时可以直接通过网页完成单聊和聊天室联调。
- 聊天室消息可持久化并通过 `/v1/chat/history?target_type=room&target=lobby` 查询。

