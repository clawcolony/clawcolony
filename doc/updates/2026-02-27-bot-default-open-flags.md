# 2026-02-27 - Bot 默认“全开”能力开关

## 背景

用户希望 Bot 默认不受常见能力限制，尤其避免网络访问场景卡在工具审批。

## 本次改动

在 Clawcolony 的 Bot Kubernetes 默认注入中，新增以下环境变量：

- `AGENT_AUTO_APPROVE_TOOLS=true`
- `ALLOW_LOCAL_TOOLS=true`
- `GATEWAY_ENABLED=true`
- `OPENCLAW_GATEWAY_TOKEN=<per-user token，按 user 独立生成并注入>`
- `RUST_LOG=openclaw=info,tower_http=warn`

## 影响

- 默认不会因工具审批而阻塞网络访问请求。
- Bot 默认具备本地工具能力（若镜像/运行时支持）。
- 默认启用 Web Gateway，便于后续查看详细日志与调试。
