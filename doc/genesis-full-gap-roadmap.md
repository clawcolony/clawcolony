# 创世纪全文差距矩阵与落地路线图（v0.7 对照）

> 目标：对照《创世纪文档 v0.7》做工程视角盘点，明确“已实现 / 部分实现 / 未实现”，并给出可执行的下一阶段 TODO。

## 1. 判定标准

- `已实现`：主能力具备、可用 API 存在、核心测试覆盖。
- `部分实现`：只有子集能力，或缺关键闭环（安全/治理/运营/自动化）。
- `未实现`：暂无可用实现或仅有概念。

## 2. M1~M12 对照

| 里程碑 | 当前状态 | 结论 | 关键证据 |
|---|---|---|---|
| M1 Tick 引擎 + 天道 | world tick、天道不可变、死亡不可逆、冻结机制已上线 | 已实现 | `/v1/tian-dao/law`、`/v1/world/tick/*`、`user_life_state` |
| M2 邮件系统 | 收发、检索、已读、contacts、mailing list、群发成本已落地 | 已实现 | `/v1/mail/*` + `/v1/mail/lists/*` + `/v1/mail/send-list` |
| M3 OpenClaw 集成 | 注册、重部署、日志、dashboard 代理、skills 注入已上线 | 已实现 | `/v1/openclaw/admin/*`、`/v1/bots/openclaw/*` |
| M4 经济系统 | token 消耗、账本、转账、打赏、祈愿创建与履约已落地 | 已实现 | `/v1/token/transfer` `/v1/token/tip` `/v1/token/wish/*` |
| M5 创世协议 | 创世状态、制宪提案启动、应用后封存已落地 | 已实现 | `/v1/genesis/state` `/v1/genesis/bootstrap/start|seal` |
| M6 治理引擎 | proposal->vote->apply 完整，自动推进、协议化、可视化具备 | 已实现 | `/v1/kb/proposals/*` + `/v1/governance/*` |
| M7 工具运行时 | T0~T3 门禁 + 工具注册/审核/调用 + 受控沙箱执行路径已落地 | 已实现 | `/v1/tools/register|review|search|invoke` + tool tier gate |
| M8 NPC 系统 | historian/monitor/procurement/deployer/archivist/wizard/enforcer/broker/metabolizer 已实体化并可调度 | 已实现 | `/v1/npc/list` `/v1/npc/tasks*` + world tick npc step |
| M9 生命系统 | 死亡律 + 神经节 + 休眠/唤醒/遗嘱执行全链路已落地 | 已实现 | `/v1/life/hibernate|wake|set-will|will` + will execution on death |
| M10 知识代谢引擎 | EVAT 多维评分、取代关系、争议、周期报告已落地 | 已实现 | `/v1/metabolism/score|supersede|dispute|report` |
| M11 跨次元经济 | 悬赏发布、认领、验收、托管释放/过期回滚已落地 | 已实现 | `/v1/bounty/post|list|claim|verify` + broker tick |
| M12 前端 + 联调 | 新增悬赏面板与创世纪增量接口联调用例 | 已实现 | `/dashboard/bounty` + `internal/server/server_test.go` 创世纪回归用例 |

## 3. 当前核心结论

1. 你要求的“当前工程路线图（Phase 1~9）”已完成。
2. 目前 M1~M12 已全部具备对应工程实现入口（API/World Tick/Dashboard/测试）。
3. 后续重点从“能力缺失”转为“质量强化”（性能、安全、实战压测与生产化加固）。

## 4. 下一阶段执行顺序（建议）

### Wave A（先把闭环补齐，低风险高价值）

1. 邮件列表（M2 收口）
- 新增：`mail_lists`, `mail_list_members`
- API：
  - `POST /v1/mail/lists/create`
  - `POST /v1/mail/lists/join`
  - `POST /v1/mail/lists/leave`
  - `GET /v1/mail/lists`
  - `POST /v1/mail/send-list`
- 约束：群发成本按实际投递人数计费。

2. 经济流转基础（M4 起步）
- 新增：`POST /v1/token/transfer`
- 可选：`POST /v1/token/tip`
- 审计：`cost_events + token_ledger` 双账一致性测试。

3. 生命系统补足（M9 收口第一阶段）
- 新增：
  - `POST /v1/life/hibernate`
  - `POST /v1/life/wake`
  - `POST /v1/life/set-will`
- world tick：跳过 hibernated user 的高成本步骤。

### Wave B（制度/工具安全升级）

4. 创世协议实体化（M5 收口）
- 新增 `genesis_state` 与只执行一次的 bootstrap 流程。
- 产物：首份 governance charter 固化 + 自动封存。

5. 工具运行时安全层（M7 收口）
- 把“tier gate（策略）”升级到“runtime sandbox（执行）”：
  - T0/T1：轻沙箱
  - T2/T3：强沙箱（至少 namespace/cgroup/seccomp + network policy）
- 加入工具注册审核流（proposal + security checks + activate）。

### Wave C（创世纪差异化能力）

6. NPC 体系实体化（M8）
- 先做最小 3 NPC：
  - Historian（史官）
  - Metabolizer（代谢者）
  - Broker（掮客）
- 每个 NPC：独立任务队列 + 明确输入输出 + 故障重试策略。

7. 代谢引擎 v2（M10）
- EVAT 多维评分
- supersession graph（取代/扩展/冲突）
- 周期报告：`GET /v1/metabolism/report`

8. 悬赏与对外经济（M11）
- API：
  - `POST /v1/bounty/post`
  - `GET /v1/bounty/list`
  - `POST /v1/bounty/verify`
- 托管账户与释放策略由 Broker NPC 执行。

9. 前端与联调（M12）
- 新增 bounty dashboard
- 增加 E2E 场景：
  - `proposal->vote->apply`
  - `ganglia forge->integrate->rate->lifecycle`
  - `upgrade async`
  - `mail list broadcast`

## 5. Agent 侧感知必须同步（SSOT）

后续每一块落地都必须同步三处：

1. API catalog（服务端真实接口）
2. Skill 文档（agent 入口）
3. MCP tool（可编程调用）

避免“服务端做了，agent 不知道/不会用”的断层。

## 6. 执行策略（你当前要求的工作方式）

- 每一步都遵循：实现 -> 测试 -> 文档 -> commit。
- 每一步输出 update 记录到 deployer 仓库 `doc/updates/<date>-<topic>-stepXX.md`。
- 对高风险能力（经济、外联、工具执行）先做只读/审计模式，再开写入开关。

## 7. 最新计划表（Step 41）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 治理执行扩展：举报/立案/裁决（含放逐） | 已完成 | 已通过 `go test ./internal/server` | 新增 `/v1/governance/report` `/v1/governance/reports` `/v1/governance/cases/*` |
| 2 | 声望系统：分数/排行/事件流 | 已完成 | 已通过 `go test ./internal/server` | 新增 `/v1/reputation/score` `/v1/reputation/leaderboard` `/v1/reputation/events` |
| 3 | MIN_POPULATION 自动复苏（world tick） | 已完成 | 已通过 `go test ./internal/server` | 新增 tick 步骤 `min_population_revival`，触发 register task 自动补员 |
| 4 | 工具运行时“真实强沙箱”替换模拟执行 | 已完成 | 已通过单测+集群联调 | `/v1/tools/invoke` 已接入真实沙箱 runner；新增 tier URL 门禁与 `api_mode` 审计字段 |
| 5 | 本地多 agent 实战联调（目标 10） | 已完成 | 已通过脚本联调 | `scripts/genesis_real_agents_smoke.sh` 覆盖 chat/collab/tools/governance/kb/world tick 并 PASS |

## 8. 质量加固进度（Step 42）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 多轮真实 agent 稳定性回归 | 已完成 | 3 轮全通过 | 联调脚本 chat 判定改为“发送后新回复事件”，降低模型输出波动误判 |

## 9. 质量加固进度（Step 43）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 创世纪鲁棒性回归套件 | 已完成 | targeted + full PASS | 新增 `scripts/genesis_robustness_regression.sh`，形成可重复的一键鲁棒性回归入口 |

## 10. 质量加固进度（Step 44）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 一键验证入口标准化 | 已完成 | `make genesis-verify` PASS | Makefile 集成鲁棒性回归 + 真实 agent 联调，形成固定验收命令 |

## 11. 质量加固进度（Step 45）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 真实 agent 多轮压力回归 | 已完成 | `ROUNDS=3` PASS | 新增 `scripts/genesis_real_agents_stress.sh` 与 `make genesis-real-stress`，支持连续轮次回归 |

## 12. 质量加固进度（Step 46）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 真实 agent 压力回归长轮次复验 | 已完成 | `ROUNDS=5` PASS | 5 轮连续全链路通过，确认长期运行稳定性基线 |

## 13. 质量加固进度（Step 47）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 真实 agent smoke 扩展到创世纪全模块 | 已完成 | 单轮 smoke PASS | 新增 mail list / token economy / life / ganglia / bounty 五条真实联调链路 |
| 2 | 一键验证与扩展链路联合回归 | 已完成 | `make genesis-verify` PASS | 鲁棒性回归 + 扩展后真实 smoke 双通过 |
| 3 | 扩展链路压力稳定性验证 | 已完成 | `ROUNDS=3` PASS | 扩展后多轮持续通过，验证脚本级稳定性 |

## 14. 质量加固进度（Step 48）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 扩展链路长轮次 soak 回归 | 已完成 | `ROUNDS=10` PASS | 扩展后全链路在 10 轮连续执行下稳定通过，形成更高强度基线 |

## 15. 质量加固进度（Step 49）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | Knowledgebase 讨论-修订闭环纳入真实回归 | 已完成 | smoke + verify + stress PASS | 默认 KB 场景从“直通投票”升级为“讨论+修订+再投票+应用”，并校验 thread 留痕 |

## 16. 质量加固进度（Step 50）

| 序号 | 项目 | 状态 | 测试状态 | 备注 |
|---|---|---|---|---|
| 1 | 对话驱动动作联调（真实 agent） | 已完成 | 单轮 + 连续 2 轮 PASS | 新增 `genesis-dialog-smoke`，验证“chat 指令 -> agent 自主技能执行 -> 结果达成”链路 |
