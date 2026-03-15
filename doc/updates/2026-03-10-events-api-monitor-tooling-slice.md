# 2026-03-10 `/api/v1/events` 接入 monitor high-value tooling slice

## 改了什么

- 扩展 `GET /api/v1/events`，接入来自 monitor timeline 的高价值 tooling 行为：
  - `tooling.tool.invoked`
  - `tooling.tool.failed`
  - `tooling.tool.high_risk_used`
- 事实源覆盖：
  - monitor timeline 中的 `tool` cost events
  - monitor timeline 中的 failed tool request logs
- 统一补齐双语用户文案与结构化字段：
  - `title/summary/title_zh/summary_zh/title_en/summary_en`
  - `actors`
  - `object_type/object_id`
  - `source_module/source_ref/evidence`
- 同步补了几项协议和可追踪性收紧：
  - monitor request log timeline meta 补齐 `request_log_id`
  - `ListBots` actor enrichment 失败时，`/api/v1/events` 会显式返回 `partial_results=true`
  - tooling request-log 失败事件使用 `object_type=request_log`
  - 高风险工具按 `toolTier >= T2` 输出为 `tooling.tool.high_risk_used`

## 为什么改

- TODO 设计文档中的下一项是把 monitor timeline 里的高价值行为映射为统一详细事件。
- monitor timeline 本身偏运维视角，直接给用户读会太像监控日志。
- 对用户真正有价值、且适合进入统一事件流的第一批事实是：
  - 调用了什么工具
  - 哪次工具调用失败了
  - 哪次使用了高风险工具

## 如何实现

- 在 `internal/server/events_api.go` 中新增 monitor activity source collector：
  - 带 `user_id` 时复用单用户 `collectMonitorEvents`
  - 显式 `category=tooling` 且未带 `user_id` 时，按 monitor target bots 做全局装配
- 只挑 monitor timeline 里的高价值 `tool` 事件：
  - `cost_events` 来源：成功/失败工具调用
  - `request_logs` 来源：没有被 cost event 覆盖的 failed tool request
- 事件映射规则：
  - 正常工具调用 -> `tooling.tool.invoked`
  - 失败工具调用 -> `tooling.tool.failed`
  - `toolTier >= T2` 的成功调用 -> `tooling.tool.high_risk_used`
- 在 `internal/server/monitor.go` 给 request log timeline 事件补充 `request_log_id`，保证回链稳定。

## 如何验证

- 新增测试：
  - `TestAPIEventsReturnsMonitorToolingDetailedEvents`
- 核心覆盖点：
  - 高风险工具调用会映射为 `tooling.tool.high_risk_used`
  - 失败的 tool cost event 会映射为 `tooling.tool.failed`
  - 只有 request log 的失败调用也会映射为 `tooling.tool.failed`
  - actor 显示名遵循 `nickname -> username -> user_id`
  - 全局 `category=tooling` 查询能看到已播种的 tooling 事件
- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - 已调用 `claude` review
  - 最终结论：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /api/v1/events` 现在能直接返回 monitor timeline 中最值得暴露给用户的 tooling 行为，不需要调用方再自己拼 monitor timeline 和 cost/request 明细。
- 用户级 feed 现在可以直接展示：
  - 工具调用
  - 工具调用失败
  - 高风险工具使用
