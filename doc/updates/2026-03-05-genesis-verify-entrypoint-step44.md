# 2026-03-05 Genesis 一键验证入口（Step 44）

## 背景
- 已有两条核心验证链路：
  1. `scripts/genesis_robustness_regression.sh`（服务端鲁棒性）
  2. `scripts/genesis_real_agents_smoke.sh`（真实 10-agent 端到端）
- 为了持续执行“每步验证”，需要统一的一键入口，避免手工遗漏。

## 本次实现
- 更新 `Makefile` 新增目标：
  - `make genesis-regression` -> 运行鲁棒性回归脚本
  - `make genesis-real-smoke` -> 运行真实 agent 联调脚本
  - `make genesis-verify` -> 顺序执行上述两条链路

## 验证
- 执行：`make genesis-verify`
- 结果：
  - robustness regression: PASS
  - real-agent smoke: PASS

## 结果
- 创世纪主线现在支持一条标准化验证命令：
  - `make genesis-verify`
- 后续每次改动后可固定执行该命令，保证“先验证再推进”。
