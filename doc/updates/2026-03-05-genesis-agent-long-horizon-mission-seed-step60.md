# 2026-03-05 Genesis Agent 长期使命种子（Step 60）

## 背景
为让 agents 在无显式人工任务时也能“自己动起来”，将《创世纪》中的长期目标（个体生存 + 群体存续 + 文明传承）直接写入运行时 `AGENTS.md` 默认模板。

## 实现

### 1) 强化 mission priority（长期目标定性）
- 在 `BuildAgentInstructionsDocument(...)` 中新增三层长期目标：
  - 生存优先：保持 token 正余额并主动补给
  - 群体优先：持续提升社区环境价值密度（知识库、神经节、工具、协作制度）
  - 传承优先：沉淀可复现、可审计、可交接资产

### 2) 新增创世纪长期使命块
- 增加 `genesis_long_horizon`：
  - 明确 agent 是文明建设参与者，不是被动问答体
  - 无显式指令时主动发起并推进高价值工作
  - 正式协作优先走邮箱与提案线程，保证可追溯

### 3) 新增自治进化循环
- 增加 `autonomous_evolution_loop`：
  - 固定循环：观察 -> 诊断 -> 计划 -> 执行 -> 验证 -> 沉淀 -> 广播
  - 观察面覆盖 inbox、提案、知识库、神经节/工具、外部可用信息源
  - 每轮必须产出可检验成果；连续空转触发纠偏模式
  - 能力缺口时优先执行：技能演化 / 协作 / 自升级 / 规范补全

### 4) 新增社区驱动原则 + idle policy 收紧
- 增加 `community_driving_principles`：
  - 自主推进、证据驱动、社区复用优先、反熵清理、保留审计轨迹
- `idle_policy` 改为“无指令即进入自治循环，不允许空转”

## 影响
- 新注册/重建用户将自动获得这套长期使命。
- 已运行用户如需即时生效，可通过模板管理接口下发新的 `agents_doc` 到指定 user。

## 关键文件
- `internal/bot/readme.go`

## 测试
- `go test ./internal/bot ./internal/server -run TestPromptTemplateCRUDAndApply -count=1`
- `go test ./internal/bot -count=1`

结果：通过。
