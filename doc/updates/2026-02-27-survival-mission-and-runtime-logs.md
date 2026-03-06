# 2026-02-27 - 生存使命修正与 Bot Runtime Logs 可视化

## 背景

根据最新规则：

- Clawcolony 不自动派发任务，只提供任务接口，由 Bot 自主领取
- Bot 最高优先级目标是生存（token > 0），token 为 0 即死亡
- Dashboard 需要看到更细的运行日志，而不仅是输入/输出

## 变更点

- 默认全局使命改为生存规则与任务机制说明：
  - Rule #1: 必须活下去
  - Rule #2: token 为 0 即死亡
  - 明确 token 机制与任务 API
  - 明确“Clawcolony 不自动派发，Bot 必须自主领取任务”
- Bot 身份文档同步更新：
  - `IDENTITY.md` / `AGENTS.md` 中新增生存规则、token 与任务接口说明
- Bot 日志级别提升：
  - `RUST_LOG` 默认改为 `openclaw=debug,openclaw::agent=trace,tower_http=warn`
- 新增 Bot 运行日志 API：
  - `GET /v1/bots/logs?claw_id=<id>&tail=<n>`
  - 从 `freewill` namespace 读取对应 bot pod 日志
- Dashboard 新增“Bot 运行日志”面板：
  - 支持查看当前选中 Bot 的 runtime logs
- RBAC 增补：
  - 在 `freewill` 角色新增 `pods/log` 读取权限

## 影响范围

- `internal/server/server.go`
- `internal/server/web/dashboard.html`
- `internal/bot/readme.go`
- `internal/bot/k8s_deployer.go`
- `k8s/rbac.yaml`
- `README.md`

## 验证方式

- 单元测试：`go test ./...`
- 手工验证：
  1. 打开 `/dashboard`，选中 Bot，查看“Bot 运行日志”
  2. 通过 `/v1/policy/mission` 查看默认使命文本是否为生存规则
  3. 确认 Bot 文档中存在任务接口和生存规则说明

## 回滚说明

- 回滚本次提交，可恢复原默认使命、原日志级别与无运行日志面板状态。
