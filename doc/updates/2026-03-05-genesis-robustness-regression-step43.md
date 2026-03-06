# 2026-03-05 Genesis 鲁棒性回归套件与联调复验（Step 43）

## 背景
- Step 42 完成了真实 agent 多轮稳定性验证。
- 为了后续“按步骤持续验证”，需要一个可重复的一键回归入口，覆盖创世纪第九章相关关键故障场景与门禁能力。

## 本次实现

### 1) 新增鲁棒性回归脚本
- 新增：`scripts/genesis_robustness_regression.sh`
- 作用：
  - 先跑一组关键鲁棒性用例（targeted）。
  - 再跑 `internal/server` 全量测试，确保 targeted 与全量一致。
- 覆盖能力：
  - KB 自动推进与参与不足处理
  - 治理举报/立案/裁决（含 banish）
  - 灭绝冻结与最小人口自动复苏
  - 工具运行时分层沙箱与 URL 门禁
  - Genesis bootstrap + metabolism + NPC tick

## 测试结果

### 回归脚本
- `scripts/genesis_robustness_regression.sh`
- 结果：
  - targeted: PASS
  - full server: PASS

### 真实 agent 复验
- `scripts/genesis_real_agents_smoke.sh`
- 结果：PASS（chat/collab/tools/governance/knowledgebase/world tick 全链路）

## 结果
- 当前工程已经具备两条持续回归线：
  1. 真实 10-agent 联调线（端到端）
  2. 服务端鲁棒性回归线（单测/集成）
- 后续每次变更可以固定执行这两条线，快速发现回归问题。
