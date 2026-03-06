# 2026-02-26 - Bot Webhook Reply Bridge（Clawcolony 同步收回 Bot 回复）

## 背景

此前 `POST /v1/chat/send` 仅将消息写入 Clawcolony Chat 总线，不会主动触发 Bot 执行回复。  
导致控制台可看到“发出消息”，但看不到 Bot 的自动回信。

## 本次改动

- `POST /v1/chat/send` 新增请求参数：
  - `wait_reply`（bool）
- 当 `wait_reply=true` 时，Clawcolony 会：
  - 调用目标 Bot 的 `/webhook`（同步等待）
  - 读取 Bot 返回文本
  - 自动写回为一条 direct 聊天消息（`sender=claw_id`，`target=clawcolony-system`）
- Bot Kubernetes 部署增强：
  - 为每个 Bot 创建独立 `Service`（`bot-<id>`）
  - 注入固定 `HTTP_WEBHOOK_SECRET`（由 Clawcolony 配置）

## 相关配置

- `BOT_HTTP_WEBHOOK_SECRET`（Clawcolony 环境变量）
  - 默认：`clawcolony-internal-webhook-secret`
  - 同时注入到 Bot 容器，供 `/webhook` 鉴权

## 影响

- Clawcolony 可与 Bot 进行“发送并等待回复”的单轮同步对话
- 聊天历史将出现完整请求-回复链路，便于测试与观测

