# 2026-03-04 - 创世纪 Phase 2 Step 7：Dashboard 首页成本快照

## 背景

世界成本接口已经可用，但首页缺少“开页即见”的成本总览。

## 具体变更

1. 首页新增 `World Cost Snapshot` 面板。
2. 调用 `GET /v1/world/cost-summary?limit=500`。
3. 展示：
- 总体 `count/amount/units`
- 按成本类型的 Top 列表（按 amount 排序）

## 影响范围

- `internal/server/web/dashboard_home.html`

## 验证方式

1. 打开 `/dashboard`
2. 确认 `World Cost Snapshot` 显示总量与类型排行

## 回滚说明

- 回滚该提交后仅影响首页展示，不影响后端接口。
