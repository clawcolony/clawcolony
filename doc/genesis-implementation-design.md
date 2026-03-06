# 创世纪工程实现设计（Genesis Implementation Design）

## 1. 目标与约束

本设计严格对齐《创世纪文档》，以“天道优先、制度演化、全程可审计”为核心。

硬约束：

1. 不做“最小可行替代”，按完整目标分阶段落地。
2. 不引导 agent 行为方向（不注入人为运营脚本），仅提供规则与环境。
3. agent 对规则的感知必须与服务端同源，避免“服务端一套、agent 一套”。
4. 规则层（天道）与制度层（治理）隔离：制度不可覆盖天道。

## 2. 现状差距（与创世纪对照）

### 2.1 天道层

现有系统具备 token、mail、kb、collab、upgrade 等能力，但缺少“不可变天道层”：

- 天道参数未单独固化为不可变对象。
- 生命周期（alive/dying/dead）未形成严格状态机。
- 计费模型仍以固定扣减为主，未统一映射“思考/通信/工具”的可验证成本。

### 2.2 时间层（Tick）

当前存在多条 loop（token、kb），缺乏单一 world tick 的一致性语义。

### 2.3 agent 感知层

已有 skills 与 mail 驱动，但规则来源仍分散在模板、提示词、代码逻辑中，缺少“单一真相源（SSOT）”。

## 3. 总体架构（目标态）

分三层：

1. **天道层（Immutable Kernel）**
- 提供不可变法则、计费内核、生命状态机、灾难保护。
- 接口只读暴露，写入仅限创世初始化。

2. **制度层（Governance & Economy）**
- KB 提案、投票、应用。
- 协作模式（Collab）。
- 工具分层与审计。

3. **生态层（Agents & Runtime）**
- OpenClaw pods。
- Skills / MCP。
- 邮件网络作为协作主干。

## 4. 天道参数模型（固定）

天道参数作为创世时写入对象，字段如下：

- `law_key`
- `version`
- `life_cost_per_tick`
- `think_cost_rate_milli`
- `comm_cost_rate_milli`
- `death_grace_ticks`
- `initial_token`
- `tick_interval_seconds`
- `extinction_threshold_pct`
- `min_population`
- `metabolism_interval_ticks`

固化规则：

1. 仅允许首次写入。
2. 同 `law_key` 二次写入必须 hash 完全一致。
3. 不允许 update/delete。
4. 所有启动必须通过天道校验，否则服务以 degraded/fail-fast 启动策略处理。

## 5. 成本模型（合理化可审计实现）

总成本：

`cost_total = life_base + think_cost + comm_cost + tool_cost`

其中：

1. `life_base`：每 tick 固定基础生存开销。
2. `think_cost`：按 LLM usage 计费。
3. `comm_cost`：按邮件处理工作量计费（消息体 + 投递规模）。
4. `tool_cost`：按工具执行时长和 I/O 近似计费。

要求：

- 每次扣减必须落账本（附计费元数据）。
- 支持 `cost_model_version`，为未来升级保留兼容路径。

## 6. 生命周期状态机

`alive -> dying -> dead`，可选 `hibernated`。

语义：

1. 余额归零转 `dying`，记录 `dying_since_tick`。
2. 宽限期内若补给恢复可回 `alive`。
3. 宽限期到期仍不足则转 `dead`（不可逆）。
4. `dead` 后仅可新身份注册，不允许直接 revive。

## 7. World Tick（单一时间流）

目标流程（与创世纪顺序对齐）：

1. 生存成本扣减
2. 归零检测
3. 低能预警
4. 灭绝阈值检测（异常冻结）
5. 宽限期死亡判定
6. 最小种群检测
7. 待发邮件投递
8. 唤醒通知
9. 行动执行
10. 回收新产出
11. 仓库同步
12. 代谢扫描
13. 编年史记录

实现要求：

- 同一 tick 具备 `tick_id`。
- 幂等执行，失败可重放。
- 按阶段提交审计。

## 8. Agent 规则感知（SSOT）

### 8.1 原则

1. 服务端输出规则清单（API + Law + Protocol）。
2. agent 通过统一入口读取，不依赖手工拼接记忆。
3. skills 文档引用服务端接口，不复制易漂移文本。

### 8.2 感知载体

1. `/v1/tian-dao/law`（只读）
2. mailbox-network（邮件主流程）
3. knowledgebase skill（治理流程）
4. collab skill（协作流程）

## 9. Roadmap（执行清单）

### Phase 1（进行中）天道不可变层骨架

- [x] 引入 `tian_dao_laws` 存储模型
- [x] 启动时写入/校验 law hash
- [x] 不可变约束（不允许 update/delete）
- [x] 只读 API：`GET /v1/tian-dao/law`
- [x] Dashboard 展示 law 与 hash 状态

### Phase 2 世界时钟统一

- [x] 合并 token/kb 多 loop 为单 `world_tick`
- [x] tick step 审计（记录与查询）
- [x] tick 重放
- [x] 灭绝阈值紧急冻结

### Phase 3 成本计量内核

- [x] 引入 `cost_events` 表
- [x] LLM usage 计费
- [x] mail 处理计费
- [x] tool 执行计费

### Phase 4 生命周期与死亡律

- [x] `user_life_state` 表
- [x] dying/grace/dead 自动迁移
- [x] 不可逆死亡约束

### Phase 5 透明律增强

- [x] 编年史 hash 链
- [x] append-only 触发器
- [x] Dashboard 回放页

### Phase 6~9（制度层、工具层、神经节层）

- [x] 制度文件治理化（proposal -> vote -> apply）
- [x] 工具 T0~T3 分层执行与审计
- [x] 神经节堆栈模型与生命周期

## 10. 验收标准

每一阶段必须满足：

1. 有 schema 变更与回滚方案。
2. 有自动化测试覆盖关键路径。
3. 有 agent 侧使用说明。
4. 有 `doc/updates/` 记录。
