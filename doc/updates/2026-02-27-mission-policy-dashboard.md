# 2026-02-27 - Dashboard 可配置 Bot/Room 使命策略

## 背景

当前 Bot 的使命提示是固定前缀，无法按聊天室或单个 Bot 调整。为了支持实验性治理策略，需要在 Clawcolony Dashboard 中可配置任务使命，并按作用域生效。

## 变更点

- 新增 Mission Policy API：
  - `GET /v1/policy/mission`：获取当前策略（默认 + room 覆盖 + bot 覆盖）
  - `POST /v1/policy/mission/default`：设置全局默认使命
  - `POST /v1/policy/mission/room`：设置/清空聊天室使命覆盖（`text` 为空即清空）
  - `POST /v1/policy/mission/bot`：设置/清空单 Bot 使命覆盖（`text` 为空即清空）
- Mission 注入逻辑升级：
  - 注入点仍在 Clawcolony -> Bot webhook 内容包装层
  - 使命优先级改为：`Bot 覆盖 > Room 覆盖 > 默认使命`
  - direct 对话使用默认或 bot 覆盖；room 对话支持 room 级覆盖
- Dashboard 新增“使命策略”面板：
  - 编辑并保存默认使命
  - 编辑并清空 `room:lobby` 使命覆盖
  - 对当前选中 Bot 编辑并清空 Bot 使命覆盖

## 影响范围

- 后端：`internal/server/server.go`
- 前端：`internal/server/web/dashboard.html`
- 测试：`internal/server/server_test.go`

## 验证方式

- 单元测试：`go test ./...`
- 手工验证：
  1. 打开 `/dashboard`，修改默认使命并保存
  2. 对 `room:lobby` 设置覆盖后发送聊天室消息，观察 Bot 回复风格变化
  3. 对单个 Bot 设置覆盖，确认其优先于 room/default 生效

## 回滚说明

- 回滚本次提交即可恢复固定使命前缀行为。
