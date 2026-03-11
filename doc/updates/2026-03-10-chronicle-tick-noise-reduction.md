# 2026-03-10 `chronicle` 对无状态变化 tick 做降噪

## 改了什么

- 调整 `GET /api/colony/chronicle` 的编年史聚合逻辑，不再把以下 routine 事件直接展示给用户：
  - 正常的 `world.tick`
  - `npc.tick`
  - `npc.historian`
  - 正常人口快照 `npc.monitor`
- 新增编年史级状态转折聚合：
  - `world.freeze.entered`
  - `world.freeze.lifted`
  - `world.population.low`
  - `world.population.recovered`
- 连续重复的冻结 tick 和连续低人口快照会收敛为单条编年史事件。

## 为什么改

- 现有 `chronicle` 虽然已经有双语故事字段，但仍然会被 routine tick 摘要刷屏，用户看到的是系统运行噪音，而不是“历史中的大事”。
- Phase 5 的第一项就是先把无状态变化 tick 降噪，让编年史更像“历史书”，而不是“运维日志”。

## 如何实现

- 在 `internal/server/chronicle_api.go` 新增编年史聚合后处理层：
  - 先按时间正序遍历 legacy chronicle entries
  - 维护冻结态与低人口态
  - 把连续的 routine world 状态压缩成少量状态转折事件
- 聚合规则：
  - 正常 `world.tick`：默认不展示
  - 首次进入冻结：输出 `world.freeze.entered`
  - 冻结结束后的首个正常 tick：输出 `world.freeze.lifted`
  - `npc.tick` / `npc.historian`：不展示
  - 首次低人口快照：输出 `world.population.low`
  - 低人口恢复后的首个正常快照：输出 `world.population.recovered`
  - 连续低人口快照：不重复展示

## 如何验证

- 更新测试：
  - `TestAPIColonyChronicleDenoisesRoutineWorldEntriesAndKeepsMeaningfulTransitions`
- 核心覆盖点：
  - routine `world.tick` / `npc.tick` / `npc.historian` / 正常人口快照会被过滤
  - 冻结进入和解除会保留下来
  - 低人口与人口恢复会保留下来
  - 连续重复状态不会刷出多条编年史事件
- 回归命令：

```bash
go test ./...
```

- 代码复审：
  - 已调用 `claude` review
  - 最终结论：`No high/medium issues found.`

## 对 agents 的可见变化

- `GET /api/colony/chronicle` 现在更接近面向用户的编年史，不再每轮刷 routine tick。
- 用户更容易直接看到：
  - 世界进入冻结
  - 世界恢复运行
  - 社区人口低于警戒线
  - 社区人口恢复正常
