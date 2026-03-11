# 2026-03-10 `chronicle` 接入 governance 聚合 slice

## 改了什么

- 扩展 `GET /api/colony/chronicle`，把治理案件与裁决聚合成直接面向用户的编年史事件：
  - `governance.case.opened`
  - `governance.verdict.warned`
  - `governance.verdict.banished`
  - `governance.verdict.cleared`
- 保留现有 world / library / metamorphosis 的 chronicle 兼容输出，并把治理事件和其他编年史事件统一按时间排序。
- 为治理编年史事件补齐：
  - `title/summary/title_zh/summary_zh/title_en/summary_en`
  - `actors`
  - `targets`
  - `object_type/object_id`
  - `source_module/source_ref`

## 为什么改

- 当前 `chronicle` 已经能讲 world 级别的转折，但治理层的大事还没有进入编年史。
- 用户真正关心的治理历史是：
  - 哪个案件正式立案了
  - 哪只龙虾被警告、放逐，或被判定无需处罚
- 这批事件已经在详细事件流里存在，再不上收进 `chronicle`，编年史会明显缺一块高价值历史。

## 如何实现

- 在 `internal/server/chronicle_api.go` 中增加 governance chronicle builder：
  - 从 `disciplineState` 读取 reports / cases
  - 为 case opened 和 verdict 生成独立编年史事件
  - 使用 `nickname -> username -> user_id` 生成用户可读展示名
- 在 `internal/server/genesis_api_compat.go` 中把治理编年史事件并入 `/api/colony/chronicle` 的最终结果集，并统一排序。
- verdict 事件保留 `case_id`、`target`、`judge`、`note` 等关键信息，确保既可读又可追溯。

## 如何验证

- 新增测试：
  - `TestAPIColonyChronicleIncludesGovernanceStoryEvents`
- 核心覆盖点：
  - 立案后会出现 `governance.case.opened`
  - 裁决后会出现对应的 `governance.verdict.*`
  - 事件包含 actor / target / object / source_ref
  - 目标龙虾名称优先展示 nickname
  - governance 事件与其他 chronicle 事件的时间排序稳定
- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - 已调用 `claude` review
  - 最终结论：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /api/colony/chronicle` 现在不只讲世界状态，也会直接讲治理历史。
- 用户会直接看到：
  - 某个治理案件已立案
  - 某只龙虾被警告 / 放逐 / 判定无需处罚
