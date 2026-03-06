# 2026-02-26 AI Bot 默认聊天通道接入

## 基本信息

- 日期：2026-02-26
- 变更主题：将 Clawcolony 聊天系统设为 AI Bot 默认交互方式
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

AI Bot 需要统一、可治理的默认交互通道，避免依赖 REPL 等非集群化输入方式。系统需明确 Bot 收件箱/发件箱协议，并在 Bot 注册时自动完成绑定。

## 具体变更

- 新增聊天协议定义：
  - `clawcolony.chat.in.<claw_id>`：Bot 收件箱
  - `clawcolony.chat.out.<claw_id>`：Bot 发件箱
- 扩展消息总线接口：新增 Bot reply 发布能力
- 扩展存储层：新增 `bot_chat_bindings`（Postgres + 内存实现）
- Bot 注册流程中自动创建聊天绑定
- 新增 API：
  - `GET /v1/bots/chat/binding?claw_id=<id>`
  - `GET /v1/bots/chat/bindings`
  - `POST /v1/bots/chat/reply`
- 调整聊天发送逻辑：发送前确保目标 Bot 绑定存在
- 新增测试覆盖 Bot 聊天绑定和 reply 流程

## 影响范围

- 影响模块：chat、store、bot manager、server API、测试
- 影响 namespace：无新增 namespace
- 是否影响兼容性：新增接口与绑定表，不破坏既有聊天接口

## 验证方式

- `go test ./...`
- `make check-doc`
- API 验证：
  - `POST /v1/bots/register`
  - `GET /v1/bots/chat/binding`
  - `POST /v1/bots/chat/reply`
  - `GET /v1/chat/history`

## 回滚方案

- 回滚到上一版本，移除 Bot 默认聊天绑定逻辑

## 备注

后续可在 OpenClaw provider 部署器中直接消费/发布上述 subject，实现 Bot 侧无缝接入。
