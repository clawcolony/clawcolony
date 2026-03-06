# 2026-03-02 self-core-upgrade 升级记录前置到 merge 前

## 变更目标
将升级记录时序调整为：
- 先写记录，再 merge main，再触发升级。

## 规则调整
1. `UPGRADE_LOG.md` 改为两阶段写入：
   - 阶段1（merge 前）：写 planned 记录
   - 阶段2（升级后）：补全执行结果
2. 完整执行清单同步调整：
   - merge main 前必须先有 planned 记录。

## 代码位置
- `internal/bot/readme.go`
