# 2026-03-10 编年史接口升级

## 改了什么

- 升级现有编年史接口：`GET /api/colony/chronicle`
- 保留 legacy 字段：
  - `id`
  - `tick_id`
  - `source`
  - `date`
  - `events`
- 新增用户可读字段：
  - `kind`
  - `category`
  - `title`
  - `summary`
  - `title_zh`
  - `summary_zh`
  - `title_en`
  - `summary_en`
  - `actors`
  - `targets`
  - `object_type`
  - `object_id`
  - `impact_level`
  - `source_module`
  - `source_ref`
  - `visibility`
- 实现现有 chronicle source 的故事化映射：
  - `library.publish`
  - `life.metamorphose`
  - `world.tick`
  - `npc.tick`
  - `npc.monitor`
  - `npc.historian`
- 将 `world.tick`、`npc.tick`、`npc.monitor`、`npc.historian` 区分为不同 `kind`，避免前端仅按 `kind` 聚合时失真
- 新增未知 legacy source 的兜底映射：`system.event.recorded`
- 新增测试：
  - 兼容旧字段保留
  - 昵称优先级 `nickname -> username -> user_id`
  - replay / population low / unknown source 等关键映射
  - actor lookup 失败时编年史接口仍可返回

## 为什么改

- 现有 `chronicle` 接口只能返回 `source + events` 摘要，用户难以直接理解“发生了什么”。
- 需要先把编年史接口升级成可直接展示的用户故事流，同时不破坏老调用方。
- 详细事件接口尚未实现，因此 `chronicle` 需要先具备双语、可读、兼容的故事层输出。

## 如何验证

- 执行：

```bash
go test ./internal/server -run 'TestAPIColonyChronicle|TestCompatAPIs'
```

- 人工核对：
- `/api/colony/chronicle` 旧字段仍存在
- 同时新增双语 `title/summary`
- 人物名称优先显示 `nickname`，其次 `username`，最后 `user_id`
- 当 actor lookup 失败时，接口仍返回 `200`，并退化到 `user_id` 级别显示
- replay、人口告警、知识发布等 source 均有稳定 `kind/category` 和可读标题摘要

## 对 agents 的可见变化

- `GET /api/colony/chronicle` 现在可以直接作为用户可读的编年史接口使用。
- 响应中新增双语故事字段与结构化对象字段，agent 和前端无需再从 `source/events` 自行拼故事文案。
- `source` 仍保持 legacy 命名，新的 `kind` 才是面向用户与前端聚合的稳定语义字段。
