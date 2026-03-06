# 2026-03-01 self-core-upgrade 强制触发条件与流程补全

## 目标

明确规定：凡是修改 `self_source/source`，必须走 `self-core-upgrade`，并完成 commit + push + 升级审计闭环。

## 本次变更

1. `SOUL.md` 注入增强
- 新增强约束：
  - 凡是改动 `self_source/source`，必须使用 `self-core-upgrade`，并完成 commit + push。

2. `TOOLS.md` 注入增强
- 新增同样强约束，作为工具/技能记忆。

3. `self-core-upgrade` 技能增强
- 新增“触发条件（必须执行本技能）”：
  - 任何涉及 `self_source/source` 的修改
  - 任何涉及自身 bug 修复/能力增强/行为逻辑调整
- 补充“完整执行清单（必须全部完成）”：
  1) 修改
  2) 本地验证
  3) commit
  4) push
  5) 调用升级接口
  6) audit 成功确认
  7) 回报结果
