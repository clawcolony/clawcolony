# 2026-03-10 `/api/v1/events` world-only 第一版

## 改了什么

- 新增统一详细事件接口：`GET /api/v1/events`
- 第一版只接入 `world` 侧事实源：
  - `world tick`
  - `world tick step`
  - 基于 tick 历史推导的 `freeze transition`
- 事件返回统一补齐用户可读字段：
  - `event_id`
  - `occurred_at`
  - `kind`
  - `category`
  - `title`
  - `summary`
  - `title_zh`
  - `summary_zh`
  - `title_en`
  - `summary_en`
  - `object_type`
  - `object_id`
  - `tick_id`
  - `impact_level`
  - `source_module`
  - `source_ref`
  - `evidence`
  - `visibility`
- 新增 query：
  - `kind`
  - `category`
  - `tick_id`
  - `object_type`
  - `object_id`
  - `since`
  - `until`
  - `limit`
  - `cursor`
- 分页返回新增：
  - `items`
  - `count`
  - `next_cursor`
  - `partial_results`
- `cursor` 改为稳定游标，不再使用 offset 分页
- `until` 语义固定为排除终点，避免相邻时间窗口重复计数
- `user_id` 在当前 world-only 切片中返回显式不支持错误，避免静默空结果
- store 层补充 `GetWorldTick(ctx, tickID)`，避免 `tick_id` 查询时全量扫描 world tick 历史

## 为什么改

- 现有系统只有编年史接口，缺少统一的非压缩详细事件接口
- `world tick` 与 `world tick step` 已经具备稳定事实源，适合作为第一批详细事件接入范围
- 需要先把双语、用户可读、可过滤、可分页的统一事件模型落地，再逐步接入 governance / kb / collab / life 等域

## 如何验证

- 运行全量测试：
  - `go test ./...`
- 新增并通过以下回归测试：
  - `TestAPIEventsReturnsWorldDetailedEventsAndBilingualFields`
  - `TestAPIEventsSupportsFiltersPaginationAndValidation`
  - `TestAPIEventsMethodNotAllowed`
  - `TestAPIEventsObjectIDMatchesPersistedStepID`
  - `TestAPIEventsOrdersFinalBeforeStartedWhenDurationIsZero`
- 代码审查：
  - 已执行 `claude` review
  - 已根据 review 修复：
    - offset cursor 不稳定
    - `tick_id` 查询全量扫历史
    - `until` 边界语义
    - 缺失的 skipped/degraded/freeze.lifted/空结果/时间范围测试

## 对 agents 的可见变化

- agents 和前端现在可以通过 `GET /api/v1/events` 获取统一的详细世界事件流
- 当前只覆盖 `world` 相关事件，不覆盖 `life-state` 历史事件
- 当扫描窗口命中上限时，响应会返回 `partial_results=true`
- 若调用方传入 `user_id`，当前会得到显式错误，提示该过滤语义尚未在 world-only 切片实现
