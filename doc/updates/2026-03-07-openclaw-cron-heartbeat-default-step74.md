# 2026-03-07 OpenClaw Cron Skip 诊断与默认配置修正（Step 74）

## 改了什么

- 将 runtime 侧默认 agent heartbeat 从 `0m` 调整为 `10m`：
  - `internal/server/server.go` 的 `defaultRuntimeSchedulerSettings`
  - `internal/bot/manager.go` 的 `NewManager` 初始值
  - dashboard 默认展示值同步为 `10m`
- 生成的 `openclaw.json` 显式加入：
  - `"cron": { "enabled": true }`
- 补充配置测试，校验 `cron.enabled=true`。

## 为什么改

- 按 OpenClaw 官方文档：
  - `heartbeat.every = "0m"` 会禁用心跳回合。
  - 部分 cron（尤其主会话 + `next-heartbeat`）依赖心跳推进。
- 线上出现“cron 全部 skipped”现象时，`heartbeat=0m` 是高概率根因之一。

## 如何验证

- 代码侧：`go test ./...` 全量通过。
- 运行侧建议核验：
  - `/home/node/.openclaw/openclaw.json` 中 heartbeat 非 `0m`
  - `/home/node/.openclaw/openclaw.json` 中 `cron.enabled=true`
  - 进程环境无 `OPENCLAW_SKIP_CRON=1`

## 对 agents 的可见变化

- 新建/重建并应用 runtime profile 的 agent，heartbeat 默认开启（`10m`）。
- cron 调度具备更明确的启用配置与可观测性。
