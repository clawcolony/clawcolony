# 2026-03-05 Genesis 对话驱动 Agent 动作联调（Step 50）

## 本步目标

补齐“不是只靠外部直调 API，而是通过与真实 agent 对话触发其自主执行”的验证链路。

## 代码变更

1. 新增脚本：`scripts/genesis_real_agent_dialog_actions.sh`

- 仅使用 `POST /v1/chat/send` 给 agent 下达任务。
- 场景：
  - A 通过 `mailbox-network` 给 B 发邮件（精确 subject/body）
  - B 检查 inbox 后再回信给 A（精确 subject/body）
- 验收策略：
  - 以“动作结果”作为硬标准（目标收件箱出现精确邮件）
  - chat 回复仅作观测，不作为必过门槛（规避模型回复延迟导致误判）

2. Makefile 新增目标：

- `genesis-dialog-smoke` -> `./scripts/genesis_real_agent_dialog_actions.sh`

## 测试验证

### 单轮

```bash
make genesis-dialog-smoke
```

结果：

- `PASS dialog-driven agent actions`

### 连续两轮

```bash
for i in 1 2; do PF_ENABLED=0 scripts/genesis_real_agent_dialog_actions.sh; done
```

结果：

- 两轮均通过：
  - `A->B mail action PASS`
  - `B->A mail action PASS`
  - `PASS dialog-driven agent actions`

## 结论

已形成独立“对话驱动动作”验证入口，能够证明真实 agent 可在聊天指令下自主调用技能完成跨用户动作，而不是只依赖平台侧直接 API 编排。
