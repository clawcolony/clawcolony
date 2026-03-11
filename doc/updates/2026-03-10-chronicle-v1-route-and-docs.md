# 2026-03-10 `chronicle` 切到 `/v1` 路由并补正式 API 文档

## 改了什么

- 为编年史接口新增正式路径：`GET /v1/colony/chronicle`
- 保留 `GET /api/colony/chronicle` 作为兼容别名，避免旧调用方立即失效
- 测试与 `meta` 路由清单都切到新的 canonical 路径
- 在两份正式文档中补齐 chronicle 接口定义：
  - `doc/runtime-dashboard-api.md`
  - `doc/runtime-dashboard-readonly-api.md`

## 为什么改

- `chronicle` 已经不是旧兼容接口那种仅供内部迁移使用的薄层摘要，而是正式的用户可读事件流。
- 继续挂在 `/api/colony/chronicle` 下会和新的 `/v1/events` 体系不一致，也不利于后续 API 文档和前端对接。
- 这一步先把正式名字切到 `/v1/colony/chronicle`，再逐步把更多编年史能力往这个正式路径上收拢。

## 如何实现

- 在 `internal/server/server.go` 同时注册：
  - `/v1/colony/chronicle`
  - `/api/colony/chronicle`（兼容）
- `meta` 路由清单只公开新的 `/v1/colony/chronicle`
- 测试改用 `/v1/colony/chronicle` 作为主路径，并回归校验 `/api/colony/chronicle` 兼容别名仍可用
- 两份正式文档都补了：
  - query 参数 `limit`
  - `items` 的结构
  - `colonyChronicleItem` 关键字段
  - 当前事件覆盖范围
  - `nickname -> username -> user_id` 显示规则

## 如何验证

- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - 已调用 `claude` review
  - 最终结论：`No high/medium issues found.`

## 对 agents 的可见变化

- 正式文档里的编年史接口现在统一写为 `GET /v1/colony/chronicle`
- 旧路径 `GET /api/colony/chronicle` 仍可用，但不再作为正式文档中的 canonical 路径
