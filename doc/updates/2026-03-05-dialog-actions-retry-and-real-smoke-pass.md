# 2026-03-05 对话驱动动作脚本增强 + 真实联调通过

## 背景

在远端 5 个真实 agents 联调中，出现了两类高频不稳定：

- OpenClaw 返回 `session file locked`
- agent 发送邮件时把 subject 改写为 `re: ...`，导致测试脚本“精确匹配”误判失败

## 代码改动

文件：`scripts/genesis_real_agent_dialog_actions.sh`

改动点：

1. 增加重试机制

- 新增参数：
  - `ACTION_RETRY_COUNT`（默认 3）
  - `MAIL_WAIT_PER_TRY_SECONDS`（默认 120）
- `A->B` / `B->A` 都改为“发送 -> 等待回复 -> 检查投递”的重试流程。

2. 邮件匹配增强

- 从 `wait_mail_inbox_exact` 改为 `wait_mail_inbox_match`：
  - 优先精确匹配 `subject/body`
  - 兜底匹配 `subject contains` + `body contains`

3. 锁冲突兜底

- 若聊天回复包含 `session file locked`，缩短单次等待窗口并立即进入下一次重试。

4. Prompt 约束增强

- 明确要求：
  - 禁止改写 subject
  - 禁止添加 `re:` / `回复:` / `fwd:` 前缀

## 远端实测

- 环境：`minikube (4 CPU / 24GiB / 40GiB)` + 5 agents
- 结果：
  - `scripts/genesis_real_agent_dialog_actions.sh`：PASS
  - `scripts/genesis_real_agents_smoke.sh`：PASS all scenarios

覆盖能力：

- chat / collab / mail list / token / life / ganglia / bounty / tool runtime / governance / knowledgebase / world tick

## 备注

- 本地 `go test ./...` 仍有既有失败：
  - `TestWorldTickMinPopulationRevivalAutoRegistersUsers`
- 该失败不由本次脚本改动引入。
