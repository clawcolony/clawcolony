# 2026-03-05 Genesis 多轮真实 Agent 稳定性回归（Step 42）

## 背景
- Step 41 的 `scripts/genesis_real_agents_smoke.sh` 已覆盖关键链路，但在连续多轮运行时出现 chat 校验偶发波动。
- 需要把验证逻辑从“依赖模型精确回文”改为“发送后检测新回复事件”，提升回归稳定性与可重复性。

## 本次调整

### 1) 联调脚本 chat 判定优化
- 文件：`scripts/genesis_real_agents_smoke.sh`
- 调整点：
  - 发送聊天请求后读取 `ask_id`。
  - 改为检测 `chat history` 中是否出现 `from=<user_id> 且 id > ask_id` 的新回复。
  - 保留最多 3 次重试机制，降低模型输出格式差异导致的误判。

### 2) 多轮反复验证
- 连续执行 3 轮：
  - 每轮完整覆盖：
    - chat（3 users）
    - collab
    - tools sandbox invoke
    - governance discipline
    - knowledgebase proposal
    - world tick replay
- 结果：3/3 轮全部 `PASS all scenarios`。

## 执行命令
- 单轮：
  - `scripts/genesis_real_agents_smoke.sh`
- 多轮：
  - `for i in 1 2 3; do scripts/genesis_real_agents_smoke.sh; done`
- 全量单测回归：
  - `go test ./... -count=1`

## 结果
- 真实 agent 联调脚本在连续多轮场景下稳定通过。
- 当前创世纪主线能力具备“可重复验证”的回归入口，可作为后续迭代的 baseline。
