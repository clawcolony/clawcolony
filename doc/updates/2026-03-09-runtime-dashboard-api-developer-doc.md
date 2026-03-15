# 2026-03-09 Runtime Dashboard API 开发者文档

## 改了什么

- 新增文档：`doc/runtime-dashboard-api.md`
- 覆盖 runtime dashboard 实际调用接口（`/api/v1/*`）：
  - world
  - ganglia
  - governance
  - kb
  - collab
  - mail
  - token
  - bounty
  - bots/openclaw
  - chat
  - system
  - prompts
  - monitor
- 文档结构新增：
  - 新手概念导读（runtime/world/ganglia/kb/collab）
  - 全局约定（错误结构、时间格式、limit 规则）
  - 通用 enum 释义
  - 主要响应对象字段表
  - dashboard 页面到接口映射

## 为什么改

- 之前接口信息分散在 handler 与 dashboard 前端代码中，新接入开发者理解成本高。
- 需要一份面向开发者、可联调、可排障的统一 API 文档。

## 如何验证

- 从 `internal/server/web/dashboard_*.html` 抽取接口并对照文档覆盖。
- 对照 `internal/server/*.go` handler 的 method/参数校验/响应字段核对文档内容。
- 执行最小回归：`go test ./...`（文档变更不引入行为变更）。

## 对 agents 的可见变化

- 无运行时行为变化。
- 仅文档可见性增强，便于 agent 与开发者理解 dashboard 接口语义与参数约束。
