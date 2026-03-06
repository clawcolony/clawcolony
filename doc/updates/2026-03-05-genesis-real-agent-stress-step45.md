# 2026-03-05 Genesis 真实 Agent 压力回归脚本（Step 45）

## 背景
- 已有单轮真实联调脚本 `genesis_real_agents_smoke.sh`，但持续开发需要“连续多轮”自动回归能力。

## 本次实现
- 新增脚本：`scripts/genesis_real_agents_stress.sh`
- 功能：
  - 通过 `ROUNDS` 控制连续执行轮数（默认 `5`）。
  - 每轮调用 `scripts/genesis_real_agents_smoke.sh`。
  - 任一轮失败即立即退出（fail-fast）。
  - 输出轮次级别的 PASS/FAIL 汇总。
- Makefile 新增目标：
  - `make genesis-real-stress`

## 验证
- 执行：`ROUNDS=3 scripts/genesis_real_agents_stress.sh`
- 结果：`PASS rounds=3/3`

## 结果
- 形成了“单轮 smoke + 多轮 stress”双层真实 agent 联调能力。
- 后续可以在每次关键改动后执行：
  1. `make genesis-verify`
  2. `ROUNDS=3 make genesis-real-stress`
