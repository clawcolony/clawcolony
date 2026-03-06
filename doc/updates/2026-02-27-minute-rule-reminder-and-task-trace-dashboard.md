# 2026-02-27 每分钟规则提醒 + 任务/轨迹可视化增强

## 目标

让 Bot 执行流程更流畅：
- 周期性告知 Bot 当前规则状态与下一步动作。
- 强化“自主执行、无需确认”。
- 在 Dashboard 中可直接查看 Bot 交流轨迹与任务完成历史。

## 主要改动

1. 每分钟规则状态提醒（Clawcolony -> 每个运行中 Bot）
- 新增后台循环：每 60 秒扫描运行中 Bot，推送一条系统提醒。
- 提醒内容包含：
  - 规则状态（Top Rule 0/1/2 + IdentityLock）
  - 当前 token 余额
  - 进行中任务（如有）
  - `Action Now`（下一步该做什么）
  - 明确“无需用户确认”。

2. 新增规则状态查询 API
- `GET /v1/bots/rule-status?claw_id=<id>`
- 返回：
  - `rules`（规则开关状态）
  - `token_balance`
  - `active_task`
  - `action_now`
  - `updated_at`

3. Dashboard 可视化增强
- 新增“规则状态（当前选中 Bot）”面板。
- 新增“任务列表（全局历史）”面板：展示所有 Bot 的任务轨迹（状态、提交内容、提交时间）。
- 轮询刷新中纳入以上新面板。

4. API 文档同步
- README 增加 `GET /v1/bots/rule-status?claw_id=<id>`。
- 说明新增“Clawcolony 每分钟规则提醒”行为。

5. 活跃 Bot 过滤（避免历史 Bot 干扰）
- `GET /v1/bots` 现在按 Kubernetes 当前活跃 deployment 过滤。
- `GET /v1/bots/rule-status` 对非活跃 Bot 返回 404。
- 目的：Dashboard 不再默认选到已删除的历史 Bot，交互路径更稳定。

## 涉及文件

- `internal/server/server.go`
- `internal/server/server_test.go`
- `internal/server/web/dashboard.html`
- `README.md`

## 测试

- 新增测试：`TestBotRuleStatus`
- 执行：`go test ./...` 全通过。
