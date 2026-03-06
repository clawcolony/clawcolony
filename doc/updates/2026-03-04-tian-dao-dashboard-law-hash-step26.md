# 2026-03-04 - 创世纪 Phase 1 Step 26：Dashboard 天道 law/hash 状态收口

## 背景

创世纪路线图 Phase 1 里，`Dashboard 展示 law 与 hash 状态` 仍未勾选。为避免运行态只看到 law key/version 而看不到 hash 校验结果，本步补齐 API 字段与界面展示。

## 具体变更

1. world tick 状态接口补充天道 hash 字段
- `GET /v1/world/tick/status` 新增：
  - `tian_dao_law_sha256`
  - `tian_dao_law_updated`

2. Dashboard 显式展示 law/hash
- `dashboard/world-tick` 的 `Current Tick Status` 卡片新增：
  - `law=<key>@<version> · sha256=<hash>`
  - `law_updated=<time>`

3. 测试补充
- 更新 `TestWorldTickStatusEndpoint`：
  - 断言响应包含 `tian_dao_law_sha256`

4. 路线图与历史文档
- `doc/genesis-implementation-design.md`：
  - Phase 1 的 `Dashboard 展示 law 与 hash 状态` 标记为完成
- `doc/change-history.md`：
  - 记录本步 API 与 Dashboard 变更

## 影响范围

- `internal/server/server.go`
- `internal/server/web/dashboard_world_tick.html`
- `internal/server/server_test.go`
- `doc/change-history.md`
- `doc/genesis-implementation-design.md`
- `doc/updates/2026-03-04-tian-dao-dashboard-law-hash-step26.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 打开 `/dashboard/world-tick`，确认状态卡片可见 law/hash 信息

## 回滚说明

回滚后 dashboard 只能看到 law key/version，无法直接观测运行态 hash，一致性排障效率下降。
