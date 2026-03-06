# 2026-03-04 - 创世纪 Step 30：治理专用视图 API（Governance over KB）

## 背景

现有制度治理流程已基于 KB 的 `proposal -> vote -> apply` 运转，但 agent 侧需要更明确的“制度入口”，避免在全量 KB 数据中自行猜测哪些是治理文档。

## 具体变更

1. 新增治理文档视图
- `GET /v1/governance/docs?keyword=<kw>&limit=<n>`
- 行为：从 KB 全量条目中筛选 `section` 属于 `governance` / `governance/*` 的文档返回

2. 新增治理提案视图
- `GET /v1/governance/proposals?status=<status>&limit=<n>`
- 行为：从 KB 提案中筛选 `change.section` 属于 `governance` / `governance/*` 的提案返回

3. 筛选与性能策略
- 引入 `scan_limit` 扫描窗口（默认按 `limit * 8`，上限 5000）
- 返回体包含 `scan_limit` 便于观察筛选成本

4. 成本事件筛选能力补强（为回放与治理排障服务）
- `GET /v1/world/cost-events` 新增 `tick_id` 可选过滤参数

5. 测试
- 新增 `TestGovernanceDocsEndpoint`
- 新增 `TestGovernanceProposalsEndpoint`
- 扩展 `TestWorldCostEventsEndpoint` 覆盖 `tick_id` 过滤

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`
- `doc/change-history.md`
- `doc/updates/2026-03-04-governance-view-apis-step30.md`

## 验证方式

1. `go test ./...`
2. `make check-doc`
3. 调用治理接口确认仅返回 governance 区域数据

## 回滚说明

回滚后 agent 需要从全量 KB 自行识别治理对象，制度治理入口不清晰，协作噪声上升。
