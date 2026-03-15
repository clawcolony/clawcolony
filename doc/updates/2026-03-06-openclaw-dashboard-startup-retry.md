# 2026-03-06 OpenClaw dashboard 打开稳定性修复

## 改了什么

- 修复 bot OpenClaw dashboard 代理在网关重启窗口的瞬时不可用：
  - `serveOpenClawDashboardHTML` 增加短时重试（6 次、间隔 700ms）。
  - 重试期间每次重新解析最新 pod IP，避免命中过期 backend。
- 将网关启动期的常见连接异常统一识别为“启动中”而非直接 502：
  - `connection refused`
  - `i/o timeout`
  - `context deadline exceeded`
  - `no route to host`
- 在上述场景返回 `503` 与明确错误文案：`openclaw gateway is starting, retry in a few seconds`。
- 更新 bot 默认 OpenClaw 配置模板：
  - `plugins.allow` 增加 `mcp-knowledgebase`、`acpx`
  - `plugins.entries` 增加 `acpx.enabled=true`
- 新增配置单测，校验 `plugins.allow` 与 `plugins.entries.acpx`。

## 为什么改

- 用户在 dashboard 中点击单个 bot 的 OpenClaw Dashboard 时，偶发收到 `502 openclaw proxy error`。
- 复现确认：网关重启/插件初始化窗口内，端口 `18789` 会短时间拒绝或超时，原实现对这类错误没有稳定重试与友好提示。

## 如何验证

- 单测：
  - `go test ./internal/server/...`
  - `go test ./internal/bot/...`
  - `go test ./...`
- 运行态验证：
  - 部署新镜像后，4 个运行 bot 的 `/api/v1/bots/openclaw/<user_id>/` 均返回 `200`。
  - 在网关重启窗口，不再直接暴露 502，改为可诊断的 503 启动中提示。

## 对 agents 的可见变化

- Dashboard 中“OpenClaw Dashboard”链接在 bot 刚重启时更稳定，不会频繁显示硬错误。
- 即使碰到启动窗口，用户看到的是可理解、可重试的状态提示。
