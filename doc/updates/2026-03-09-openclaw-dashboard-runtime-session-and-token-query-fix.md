# 2026-03-09 OpenClaw Dashboard 默认会话与 token 查询修复

## 改了什么

- 调整 OpenClaw dashboard bootstrap 注入策略：
  - 新增 `runtimeSession=runtime-chat-<user_id>` 默认值。
  - 当本地 `sessionKey` 为空、`main` 或 legacy 值 `agent:main:main` 时，自动切到 `runtimeSession`。
  - 若 `user_id` 缺失，回退 `runtimeSession=main`。
- 修复 runtime dashboard chat 页面请求 OpenClaw token 时未带 `user_id` 的问题：
  - `GET /v1/system/openclaw-dashboard-config` 改为携带 `?user_id=<id>`。
  - server 侧将 `user_id` 设为必填并校验 user 存在，避免无范围 token 查询。
  - 前端改为按 user 维护 token 缓存，构建 OpenClaw 链接时按目标 user 读取 token。
- 新增单测覆盖 bootstrap 注入行为：
  - `TestOpenClawBootstrapScriptDefaultsRuntimeChatSession`
  - `TestOpenClawBootstrapScriptFallbackSessionWhenUserMissing`

## 为什么改

- OpenClaw dashboard 默认落在 `main` 会话，容易与 heartbeat/cron 共享会话，导致人工对话体验混杂。
- chat 页未传 `user_id` 获取 token 时返回空 token，OpenClaw 链接上无法携带正确 token，增加连接失败和排障成本。

## 如何验证

- 单测：
  - `go test ./internal/server -run 'TestOpenClawBootstrapScriptDefaultsRuntimeChatSession|TestOpenClawBootstrapScriptFallbackSessionWhenUserMissing' -count=1`
  - `go test ./internal/server/...`
- 运行态手工验证：
  - 打开 `/v1/bots/openclaw/<user_id>/`，确认 `openclaw.control.settings.v1.sessionKey` 在空值或 `main` 时会改为 `runtime-chat-<user_id>`。
  - 打开 `/dashboard/chat`，点击 OpenClaw Dashboard 链接，确认 `/v1/system/openclaw-dashboard-config?user_id=<id>` 返回非空 token（对应 active user）。

## 对 agents 的可见变化

- OpenClaw dashboard 默认会话更贴近 runtime chat（`runtime-chat-<user_id>`），降低与系统 heartbeat 共用 `main` 会话的干扰。
- dashboard chat 页面打开 OpenClaw 链接时，token 绑定到具体 user，连接稳定性提升。
