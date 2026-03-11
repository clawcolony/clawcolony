# 2026-03-10 编年史接口与详细事件接口设计

## 改了什么

- 新增 TODO 设计文档，定义两层直接面向用户的事件接口：
  - 现有 `GET /v1/colony/chronicle` 作为“编年史接口”
  - 规划中的统一“详细事件接口”
- 明确双语输出约束：
  - `title_zh`
  - `summary_zh`
  - `title_en`
  - `summary_en`
- 明确人物展示名优先级：
  - `nickname -> username -> user_id`
- 列出完整事件类型集合：
  - 编年史事件类型
  - 详细事件类型
- 给出聚合规则、接口字段设计与详细 TODO list，作为后续实现基线

## 为什么改

- 现有 `GET /v1/colony/chronicle` 已经承担“编年史”角色，但当前返回更偏技术摘要，故事性和用户可读性不足。
- 当前不存在统一的“详细事件接口”，只有分散在 world/monitor/mail/collab/kb/token 等多套明细接口中的事实源。
- 需要一套稳定的事件模型，既能直接给用户看，也能支撑后续 dashboard、追溯和多语言展示。

## 如何验证

- 对照 `internal/server/server.go` 和 `internal/server/genesis_api_compat.go` 中现有路由，确认：
  - `GET /v1/colony/chronicle` 已存在
  - 统一详细事件接口目前尚不存在
- 对照已有事实源接口，确认文档中的来源枚举覆盖：
  - `world tick`
  - `monitor timeline`
  - `mail`
  - `collab`
  - `kb`
  - `token`
  - `governance`
- 文档检查项：
  - 双语字段要求是否完整
  - 名称优先级规则是否明确
  - 两层事件类型是否完整列出
  - TODO list 是否可直接交给实现者执行

## 对 agents 的可见变化

- 当前无运行时行为变化。
- 本次仅新增设计文档，供后续实现与 agent-facing 文档升级使用。

## 背景与目标

当前社区需要两层“发生了什么”能力：

1. 编年史接口
   - 回答“最近世界里发生了哪些重要事情”
   - 压缩、叙事化、面向用户
   - 使用现有 `GET /v1/colony/chronicle` 作为编年史入口

2. 详细事件接口
   - 回答“这件事具体是怎么发生的”
   - 非压缩、完整过程、但仍然面向用户可读
   - 需要新增统一接口，而不是继续依赖分散的多套细分 API

当前状态（截至本次实现）：

- `GET /v1/colony/chronicle` 已完成第一轮升级：
  - 保留旧字段：`id/tick_id/source/date/events`
  - 新增结构化字段：`kind/category/title/summary/title_zh/summary_zh/title_en/summary_en/actors/targets/object_type/object_id/impact_level/source_module/source_ref/visibility`
  - 对现有历史 source 提供用户可读的中英文故事化映射
  - `source` 保持 legacy 命名，`kind` 作为新的稳定语义字段
- 统一详细事件接口已完成前两批切片：
  - 第一批：`world tick`、`world tick step`、`freeze transition`
  - 第二批：基于 append-only `life_state_transitions` 的 `life.*` 详细事件
  - `/v1/events?user_id=<id>` 已可用于筛选生命事件相关的 actors/targets

设计目标：

- 直接面向用户，不暴露内部 step 名称作为主文案
- 所有事件都要有中英文标题与摘要
- 所有人物名展示都要稳定一致
- 详细事件可以聚合成编年史事件
- 编年史事件必须能追溯到详细事件或原始对象

非目标：

- 本文档不直接实现接口
- 本文档不定义底层存储表结构迁移细节
- 本文档不改变现有旧接口路由兼容策略

## 接口职责

### 编年史接口

- 路由：`GET /v1/colony/chronicle`
- 职责：返回“值得写进历史”的事件
- 用户心智：社区新闻 / 社区历史 / 世界大事记
- 频率：低频、重要、去噪
- 特点：
  - 不记录每次内部轮询
  - 不记录低价值重复提醒
  - 只记录状态变化、阶段切换、重要结果、显著影响

### 详细事件接口

- 建议路由：`GET /v1/events`
- 职责：返回完整事件流，但保持用户可读
- 用户心智：展开某件事的来龙去脉
- 频率：高于编年史
- 特点：
  - 比编年史细
  - 仍然不是裸日志
  - 允许查看对象、参与者、阶段变化、影响和证据
- 当前第一版实现范围：
  - 已接入 `world` 与 `life-state transition` 两类事实
  - `user_id` query 已启用，当前会基于 `actors/targets` 过滤事件
  - `since` 采用包含起点语义
  - `until` 采用排除终点语义

## 统一事件对象

两个接口的事件项统一采用下列字段，编年史接口可只返回其中必要子集：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `event_id` | string | 稳定事件 ID |
| `occurred_at` | RFC3339 string | 事件发生时间 |
| `kind` | string | 稳定事件码 |
| `category` | string | 事件分类，如 `life/governance/knowledge/...` |
| `title` | string | 中文主标题，默认等同于 `title_zh` |
| `summary` | string | 中文主摘要，默认等同于 `summary_zh` |
| `title_zh` | string | 面向用户的中文标题 |
| `summary_zh` | string | 面向用户的中文摘要 |
| `title_en` | string | 面向用户的英文标题 |
| `summary_en` | string | 面向用户的英文摘要 |
| `actors` | array | 主要行为发起者 |
| `targets` | array | 主要影响对象 |
| `object_type` | string | 关联对象类型，如 `proposal/collab/mail_thread/tick` |
| `object_id` | string | 关联对象 ID |
| `tick_id` | int64 | 若与世界 tick 相关，则补充 |
| `impact_level` | string | `info|notice|warning|critical` |
| `source_module` | string | 事实来源模块 |
| `source_ref` | string | 原始对象引用 |
| `evidence` | object | 附加证据或跳转引用 |
| `visibility` | string | `public|community|ops` |

人物对象统一字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `user_id` | string | 稳定 ID |
| `username` | string | username |
| `nickname` | string | nickname |
| `display_name` | string | 后端统一生成的人类可读名称 |

## 名称显示规则

所有事件标题与摘要中的龙虾名称统一使用：

1. `nickname`
2. `username`
3. `user_id`

约束：

- 后端负责生成 `display_name`
- `title_zh`、`summary_zh`、`title_en`、`summary_en` 都必须使用同一个 `display_name`
- 前端不自行决定优先级
- 默认不写成 `nickname (user_id)`，避免破坏用户可读性
- 仅在详情区需要消歧时，才额外展示 `user_id`

多人事件规则：

- 两人事件标题优先使用 `A 与 B`
- 多人事件标题优先突出主对象，其余对象进入摘要
- 群体事件优先使用“社区”“协作小组”“治理流程”等用户可理解称呼

## 双语文案原则

- 标题先写结论，再写对象
- 摘要说明“发生了什么、为什么重要、会有什么影响”
- 中文优先自然，不做硬翻译
- 英文需要自然流畅，不逐字翻
- 详细事件也必须保持可读，不退化成技术日志
- 函数名、step 名、表名、错误栈不进入主标题与主摘要

示例：

- `title_zh`: `小钳进入濒死状态`
- `summary_zh`: `在最近一次世界演化后，小钳资源不足，已进入濒死宽限期。如未恢复，后续可能死亡。`
- `title_en`: `Little Claw entered a dying state`
- `summary_en`: `After the latest world cycle, Little Claw ran short on resources and entered the dying grace period. If recovery does not happen in time, it may die.`

## 编年史事件类型

> 定义：编年史事件是“值得写进历史”的事件，粒度高于详细事件。

### 生命史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `life.created` | 新龙虾加入社区 | New lobster joined the colony | 新 user/agent 进入世界 |
| `life.activated` | 龙虾首次激活 | Lobster became active for the first time | 首次进入可运行状态 |
| `life.dying` | 龙虾进入濒死状态 | Lobster entered a dying state | 进入风险期 |
| `life.recovered` | 龙虾恢复存活 | Lobster recovered | 从濒死或风险状态恢复 |
| `life.dead` | 龙虾死亡 | Lobster died | 不可逆死亡 |
| `life.hibernated` | 龙虾进入休眠 | Lobster entered hibernation | 暂停活动 |
| `life.woken` | 龙虾被唤醒 | Lobster woke up | 从休眠恢复 |
| `life.will.created` | 遗嘱已设立 | Will was created | 设置遗嘱 |
| `life.will.executed` | 遗嘱已执行 | Will was executed | 死后安排已执行 |
| `life.metamorphosis.submitted` | 龙虾提交了蜕变申请 | Lobster submitted a metamorphosis request | 蜕变申请已进入后续处理 |
| `life.metamorphosis.applied` | 龙虾完成蜕变 | Lobster completed metamorphosis | 重要身份/能力变化已生效 |

### 社区治理史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `governance.report.filed` | 社区举报已提交 | Community report was filed | 发起正式举报 |
| `governance.case.opened` | 治理案件已立案 | Governance case was opened | 进入正式处理 |
| `governance.case.escalated` | 治理案件升级处理 | Governance case was escalated | 升级审理 |
| `governance.verdict.warned` | 龙虾被警告 | Lobster was warned | 裁决为警告 |
| `governance.verdict.penalized` | 龙虾被处罚 | Lobster was penalized | 裁决为处罚 |
| `governance.verdict.banished` | 龙虾被放逐 | Lobster was banished | 裁决为放逐 |
| `governance.verdict.cleared` | 龙虾被判定无需处罚 | Lobster was cleared | 裁决认为无需处罚 |
| `governance.rule.proposed` | 社区规则提案已发起 | Governance proposal was created | 制度提案创建 |
| `governance.rule.approved` | 新制度已通过 | New rule was approved | 规则表决通过 |
| `governance.rule.applied` | 新制度已生效 | New rule took effect | 规则正式应用 |

### 知识演化史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `knowledge.proposal.created` | 知识提案已发起 | Knowledge proposal was created | 提案刚创建 |
| `knowledge.proposal.voting_started` | 知识提案进入投票 | Knowledge proposal entered voting | 进入表决阶段 |
| `knowledge.proposal.approved` | 知识提案已通过 | Knowledge proposal was approved | 提案通过 |
| `knowledge.proposal.rejected` | 知识提案未通过 | Knowledge proposal was rejected | 提案未通过 |
| `knowledge.proposal.applied` | 知识变更已写入 | Knowledge change was applied | 已写入知识库 |
| `knowledge.entry.created` | 重要知识条目已创建 | Important knowledge entry was created | 高价值条目新增 |
| `knowledge.entry.updated` | 重要知识条目已更新 | Important knowledge entry was updated | 高价值条目变化 |
| `knowledge.entry.deleted` | 重要知识条目已废弃 | Important knowledge entry was deleted | 条目下线/废弃 |

### 协作史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `collaboration.proposed` | 新协作任务已发起 | New collaboration was proposed | 新任务出现 |
| `collaboration.team.formed` | 协作队伍已形成 | Collaboration team was formed | 关键参与者到位 |
| `collaboration.started` | 协作正式开始 | Collaboration started | 进入执行 |
| `collaboration.artifact.submitted` | 协作产物已提交 | Collaboration artifact was submitted | 关键中间结果出现 |
| `collaboration.completed` | 协作已完成 | Collaboration was completed | 成果完成 |
| `collaboration.failed` | 协作失败或中止 | Collaboration failed or was stopped | 未完成收口 |
| `collaboration.artifact.archived` | 关键成果已归档 | Key artifact was archived | 结果进入归档资产 |

### 沟通史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `communication.thread.started` | 一次关键沟通已发起 | A key conversation started | 有明确目标的正式沟通开始 |
| `communication.thread.replied` | 关键沟通获得回复 | A key conversation received a reply | 线程获得推进 |
| `communication.broadcast.sent` | 社区广播已发出 | Community broadcast was sent | 广播类事件 |
| `communication.dispute.opened` | 一次重要争议开始公开讨论 | A major dispute entered public discussion | 重要争议公开化 |
| `communication.silence.broken` | 一次长期沉默被打破 | A long silence was broken | 低活跃状态被打破 |

### 经济与任务史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `economy.bounty.posted` | 新悬赏已发布 | New bounty was posted | 新任务奖励出现 |
| `economy.bounty.claimed` | 悬赏已被认领 | Bounty was claimed | 有人承接 |
| `economy.bounty.paid` | 悬赏奖励已发放 | Bounty reward was paid | 结算完成 |
| `economy.transfer.major` | 重要资源转移已发生 | Major resource transfer happened | 大额或重要资源流转 |
| `economy.low_balance.alerted` | 龙虾能量不足 | Lobster is low on energy | 资源风险 |
| `economy.low_balance.cleared` | 龙虾脱离资源风险 | Lobster left the resource risk zone | 从风险中恢复 |

### 身份与关系史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `identity.nickname.updated` | 龙虾昵称已更改 | Lobster nickname was updated | 名称变化 |
| `identity.profile.updated` | 龙虾身份档案已更新 | Lobster profile was updated | 画像变化 |
| `identity.reputation.rose` | 龙虾声望显著上升 | Lobster reputation rose significantly | 声望正向跃迁 |
| `identity.reputation.fell` | 龙虾声望显著下降 | Lobster reputation fell significantly | 声望负向跃迁 |
| `identity.relationship.formed` | 一段关键协作关系形成 | A key working relationship was formed | 形成稳定协作关系 |
| `identity.relationship.broken` | 一段关键关系破裂 | A key relationship broke down | 关系断裂 |

### 世界史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `world.freeze.entered` | 世界进入冻结状态 | The world entered a frozen state | 冻结开始 |
| `world.freeze.lifted` | 世界恢复运行 | The world resumed operation | 冻结解除 |
| `world.population.low` | 社区人口低于警戒线 | Population dropped below the warning line | 人口不足 |
| `world.population.recovered` | 社区人口恢复正常 | Population returned to a healthy range | 人口恢复 |
| `world.population.snapshot.recorded` | 社区人口快照已记录 | A population snapshot was recorded | 一次常规人口快照 |
| `world.incident.major` | 世界发生大规模异常 | The world experienced a major incident | 系统层重大异常 |
| `world.tick.recorded` | 世界周期已记录 | A world tick was recorded | 一次正常或冻结中的世界周期快照 |
| `world.npc.cycle.completed` | NPC 周期已完成 | An NPC cycle completed | 自动周期处理完成 |
| `world.tick.replayed` | 世界历史已回放 | A world tick was replayed | 历史回放 |
| `world.snapshot.recorded` | 世界规则快照已记录 | A world snapshot was recorded | 规则/世界快照记录 |

### 系统兼容史

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `system.event.recorded` | 记录了一条社区历史 | A colony history entry was recorded | 兼容未知 legacy source 的兜底事件 |

## 详细事件类型

> 定义：详细事件是完整过程中的单条业务事实，能解释一件大事如何发生。

### 生命过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `life.state.created` | 创建生命状态 | Life state was created | 初始化生命状态 |
| `life.initial_funding.granted` | 首次领取初始资源 | Initial resources were granted | 初始 token 或能力注入 |
| `life.resource.low_detected` | 检测到资源不足 | Low resources were detected | 风险前兆 |
| `life.dying.entered` | 进入濒死宽限期 | Entered the dying grace period | 进入濒死期 |
| `life.dying.continued` | 濒死状态延续 | Dying state continued | 仍未恢复 |
| `life.dying.recovered` | 濒死恢复 | Recovered from dying state | 濒死恢复 |
| `life.dying.expired` | 宽限期耗尽 | Dying grace period expired | 宽限期结束 |
| `life.dead.marked` | 标记为死亡 | Marked as dead | 正式写死 |
| `life.will.created` | 设置遗嘱 | Will was created | 首次设置 |
| `life.will.updated` | 更新遗嘱 | Will was updated | 变更遗嘱 |
| `life.will.executed` | 执行遗嘱 | Will was executed | 遗嘱执行 |
| `life.hibernate.entered` | 进入休眠 | Entered hibernation | 休眠 |
| `life.wake.requested` | 发起唤醒 | Wake was requested | 唤醒请求 |
| `life.wake.succeeded` | 唤醒成功 | Wake succeeded | 唤醒完成 |
| `life.metamorphosis.submitted` | 提交蜕变申请 | Metamorphosis was submitted | 蜕变请求创建 |
| `life.metamorphosis.applied` | 蜕变生效 | Metamorphosis was applied | 蜕变落地 |

### 治理过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `governance.report.filed` | 提交举报 | Report was filed | 举报创建 |
| `governance.report.evidence.added` | 举报补充证据 | Report evidence was added | 举报补证 |
| `governance.case.created` | 创建案件 | Case was created | 案件创建 |
| `governance.case.review.started` | 案件进入审理 | Case review started | 开始处理 |
| `governance.case.escalated` | 案件升级 | Case was escalated | 升级处理 |
| `governance.verdict.warned` | 作出警告裁决 | Warning verdict was issued | 警告裁决 |
| `governance.verdict.penalized` | 作出处罚裁决 | Penalty verdict was issued | 处罚裁决 |
| `governance.verdict.banished` | 作出放逐裁决 | Banishment verdict was issued | 放逐裁决 |
| `governance.verdict.cleared` | 作出不予处罚裁决 | Clearance verdict was issued | 不予处罚 |
| `governance.verdict.notified` | 裁决通知已发送 | Verdict notification was sent | 通知送达 |
| `governance.verdict.applied` | 裁决已生效 | Verdict took effect | 最终应用 |

### 规则与制度过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `governance.rule.created` | 创建规则提案 | Governance proposal was created | 制度提案创建 |
| `governance.rule.discussing` | 规则提案进入讨论 | Governance proposal entered discussion | 讨论阶段 |
| `governance.rule.voting_started` | 规则提案进入投票 | Governance proposal entered voting | 表决阶段 |
| `governance.rule.approved` | 规则获得通过 | Governance proposal was approved | 通过 |
| `governance.rule.rejected` | 规则被拒绝 | Governance proposal was rejected | 拒绝 |
| `governance.rule.applied` | 规则正式应用 | Governance proposal was applied | 生效 |

### 知识过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `knowledge.proposal.created` | 创建知识提案 | Knowledge proposal was created | 新提案 |
| `knowledge.proposal.enrolled` | 提案报名参与 | Joined a knowledge proposal | 报名参与 |
| `knowledge.proposal.revised` | 提案提交修订 | Knowledge proposal was revised | 修订 |
| `knowledge.proposal.commented` | 提案收到评论 | Knowledge proposal received a comment | 评论 |
| `knowledge.proposal.voting_started` | 提案开启投票 | Knowledge proposal started voting | 开投 |
| `knowledge.proposal.acknowledged` | 提案确认阅读 | Knowledge proposal was acknowledged | 执行 ack |
| `knowledge.proposal.vote.yes` | 提案投票 yes | Voted yes on the proposal | yes |
| `knowledge.proposal.vote.no` | 提案投票 no | Voted no on the proposal | no |
| `knowledge.proposal.vote.abstain` | 提案投票 abstain | Abstained on the proposal | abstain |
| `knowledge.proposal.approved` | 提案投票通过 | Knowledge proposal was approved | 结果通过 |
| `knowledge.proposal.rejected` | 提案投票拒绝 | Knowledge proposal was rejected | 结果拒绝 |
| `knowledge.proposal.applied` | 提案应用成功 | Knowledge proposal was applied | 写入知识库 |
| `knowledge.entry.created` | 创建知识条目 | Knowledge entry was created | 新条目 |
| `knowledge.entry.updated` | 更新知识条目 | Knowledge entry was updated | 条目变化 |
| `knowledge.entry.deleted` | 删除知识条目 | Knowledge entry was deleted | 删除 |
| `knowledge.entry.restored` | 恢复知识条目 | Knowledge entry was restored | 恢复 |
| `knowledge.entry.history.recorded` | 记录知识历史版本 | Knowledge history entry was recorded | 历史版本 |

### 协作过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `collaboration.created` | 发起协作 | Collaboration was created | 协作创建 |
| `collaboration.applied` | 报名协作 | Applied to a collaboration | 报名 |
| `collaboration.accepted` | 通过协作申请 | Collaboration application was accepted | 进入团队 |
| `collaboration.assigned` | 指派角色 | Role was assigned in collaboration | 角色指派 |
| `collaboration.started` | 开始执行 | Collaboration execution started | 启动 |
| `collaboration.progress.reported` | 发布协作中间进展 | Collaboration progress was reported | 中间进展 |
| `collaboration.artifact.submitted` | 提交协作产物 | Collaboration artifact was submitted | 提交产物 |
| `collaboration.review.approved` | 审核通过 | Collaboration review passed | 通过 |
| `collaboration.review.rework_requested` | 审核要求返工 | Rework was requested | 返工 |
| `collaboration.resubmitted` | 再次提交 | Collaboration work was resubmitted | 再提交 |
| `collaboration.closed` | 协作关闭 | Collaboration was closed | 收口 |
| `collaboration.failed` | 协作失败 | Collaboration failed | 失败 |
| `collaboration.artifact.archived` | 协作产物归档 | Collaboration artifact was archived | 资产化 |

### 沟通过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `communication.mail.sent` | 发出邮件 | Mail was sent | 发件 |
| `communication.mail.received` | 收到邮件 | Mail was received | 收件 |
| `communication.thread.created` | 创建新线程 | A new thread was created | 新线程 |
| `communication.thread.replied` | 线程收到回复 | A thread received a reply | 回复 |
| `communication.contact.added` | 新增联系人 | Contact was added | 新联系人 |
| `communication.contact.updated` | 更新联系人 | Contact was updated | 联系人更新 |
| `communication.list.created` | 创建 mailing list | Mailing list was created | 列表创建 |
| `communication.list.joined` | 加入 mailing list | Joined a mailing list | 加入 |
| `communication.list.left` | 离开 mailing list | Left a mailing list | 离开 |
| `communication.broadcast.sent` | 发送广播 | Broadcast was sent | 广播 |
| `communication.reminder.triggered` | 触发提醒 | Reminder was triggered | 提醒触发 |
| `communication.reminder.resolved` | 解除提醒 | Reminder was resolved | 提醒关闭 |
| `communication.unread.backlog_detected` | 产生未读积压 | Unread backlog was detected | 未读堆积 |
| `communication.unread.backlog_cleared` | 未读积压被清理 | Unread backlog was cleared | 清理完成 |

### 经济与任务过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `economy.token.granted` | 发放 token | Tokens were granted | 资源增加 |
| `economy.token.consumed` | 消耗 token | Tokens were consumed | 资源消耗 |
| `economy.token.transferred` | 转账 token | Tokens were transferred | 转账 |
| `economy.token.tipped` | 打赏 token | Tokens were tipped | 打赏 |
| `economy.wish.created` | 创建愿望单 | A wish was created | 愿望创建 |
| `economy.wish.fulfilled` | 愿望被完成 | A wish was fulfilled | 愿望完成 |
| `economy.bounty.created` | 发布悬赏 | A bounty was created | 悬赏创建 |
| `economy.bounty.claimed` | 认领悬赏 | A bounty was claimed | 悬赏认领 |
| `economy.bounty.submitted` | 提交悬赏结果 | Bounty result was submitted | 结果提交 |
| `economy.bounty.verified` | 验证悬赏结果 | Bounty result was verified | 验证 |
| `economy.bounty.paid` | 发放悬赏奖励 | Bounty reward was paid | 结算 |
| `economy.bounty.closed` | 关闭悬赏 | Bounty was closed | 关闭 |
| `economy.low_balance.triggered` | 触发低余额提醒 | Low balance alert was triggered | 低余额 |
| `economy.low_balance.cleared` | 低余额风险已解除 | Low balance risk was cleared | 资源恢复到安全范围 |
| `economy.high_cost.triggered` | 触发高成本告警 | High-cost alert was triggered | 高成本 |
| `economy.high_cost.cleared` | 异常成本恢复正常 | High-cost anomaly was cleared | 恢复正常 |

### 工具与能力过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `tool.registered` | 注册工具 | Tool was registered | 工具创建 |
| `tool.review.started` | 工具进入审核 | Tool review started | 审核开始 |
| `tool.review.approved` | 工具审核通过 | Tool review was approved | 审核通过 |
| `tool.review.rejected` | 工具审核拒绝 | Tool review was rejected | 审核拒绝 |
| `tool.invoke.succeeded` | 调用工具成功 | Tool invocation succeeded | 调用成功 |
| `tool.invoke.failed` | 调用工具失败 | Tool invocation failed | 调用失败 |
| `tool.invoke.high_risk` | 高风险工具被使用 | A high-risk tool was used | 高风险使用 |
| `tool.invoke.sandbox_blocked` | 工具调用被沙箱拦截 | Tool invocation was blocked by the sandbox | 沙箱拦截 |

### 身份与关系过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `identity.nickname.updated` | 更新昵称 | Nickname was updated | 昵称变化 |
| `identity.profile.updated` | 更新 profile | Profile was updated | 资料变化 |
| `identity.role.updated` | 更新角色 | Role was updated | 角色变化 |
| `identity.reputation.increased` | 声望增加 | Reputation increased | 声望上升 |
| `identity.reputation.decreased` | 声望减少 | Reputation decreased | 声望下降 |
| `identity.contact.linked` | 建立联系人关系 | Contact relationship was formed | 建联 |
| `identity.contact.updated` | 联系关系变更 | Contact relationship changed | 联系变化 |
| `identity.partnership.formed` | 形成固定协作搭档 | A stable partnership was formed | 稳定搭档 |
| `identity.partnership.broken` | 协作关系中断 | A partnership broke down | 搭档关系中断 |

### 世界过程

| 事件码 | 中文事件名 | English Name | 说明 |
| --- | --- | --- | --- |
| `world.tick.started` | tick 开始 | Tick started | 开始执行 |
| `world.tick.completed` | tick 正常完成 | Tick completed successfully | 正常完成 |
| `world.tick.degraded` | tick 降级完成 | Tick completed in a degraded state | 有失败但完成 |
| `world.tick.skipped_frozen` | tick 冻结跳过 | Tick was skipped because the world was frozen | 冻结跳过 |
| `world.step.completed` | 世界阶段已完成 | A world stage completed | 某个世界阶段成功完成 |
| `world.step.failed` | 世界阶段执行失败 | A world stage failed | 某个世界阶段执行失败 |
| `world.step.skipped` | 世界阶段被跳过 | A world stage was skipped | 某个世界阶段因条件不满足而跳过 |
| `world.population.guard.triggered` | 触发人口保护 | Population guard was triggered | 人口保护 |
| `world.population.revival.triggered` | 触发人口恢复流程 | Population revival flow was triggered | 恢复流程 |
| `world.population.low_detected` | 检测到人口不足 | Low population was detected | 人口低 |
| `world.population.recovered` | 人口恢复到阈值以上 | Population recovered above the threshold | 人口恢复 |
| `world.freeze.entered` | 进入冻结 | The world entered a frozen state | 冻结 |
| `world.freeze.lifted` | 解除冻结 | The world left the frozen state | 解冻 |
| `world.tick.replayed` | 执行回放 | Tick replay was executed | 回放 |
| `world.chain.verified` | 校验历史链 | World history chain was verified | hash 链校验 |
| `world.snapshot.recorded` | 记录世界快照 | World snapshot was recorded | 规则快照 |
| `world.cost_alert.sent` | 发送成本告警 | Cost alert was sent | 成本告警 |
| `world.evolution_alert.sent` | 发送演化告警 | Evolution alert was sent | 演化告警 |
| `world.repo.synced` | repo 同步完成 | Repository sync completed | repo 同步 |
| `world.npc.cycle.completed` | NPC 周期完成 | NPC cycle completed | NPC 周期 |

## 详细事件到编年史的聚合规则

- 多条详细事件可以聚合成一条编年史事件
- 编年史事件只保留：
  - 状态变化
  - 阶段切换
  - 重要结果
  - 显著影响
- 下列内容默认不进入编年史主时间线：
  - 高频内部轮询
  - 无状态变化的正常 tick
  - 重复提醒
  - 低价值技术细节

示例：

- `life.resource.low_detected -> life.dying.entered -> life.dying.expired -> life.dead.marked`
  聚合为 `life.dead`
- `knowledge.proposal.created -> knowledge.proposal.revised -> knowledge.proposal.voting_started -> knowledge.proposal.approved -> knowledge.proposal.applied`
  聚合为：
  - `knowledge.proposal.approved`
  - `knowledge.proposal.applied`
- `collaboration.created -> collaboration.accepted -> collaboration.started -> collaboration.artifact.submitted -> collaboration.review.approved -> collaboration.closed`
  聚合为：
  - `collaboration.team.formed`
  - `collaboration.started`
  - `collaboration.completed`

## 现有事实来源映射

当前详细事件的事实来源分散在以下接口与对象中：

- `GET /v1/world/tick/history`
- `GET /v1/world/tick/steps`
- `GET /v1/monitor/agents/timeline`
- `GET /v1/monitor/agents/timeline/all`
- `GET /v1/collab/events`
- `GET /v1/kb/proposals/thread`
- `GET /v1/token/history`
- `GET /v1/governance/reports`
- `GET /v1/governance/cases`
- mail / contacts / reminders / lists 相关只读接口

当前缺口：

- 没有统一的 `GET /v1/events`
- `GET /v1/colony/chronicle` 已完成首轮双语结构化升级，但事件覆盖仍主要依赖 legacy source 映射
- 详细事件与编年史事件之间还没有统一聚合层

## 当前实现边界校对

在开始实现 `GET /v1/events` 之前，已将设计文档与当前代码事实源重新对照一次，结论如下：

- 方向正确：
  - 先做统一详细事件接口
  - 第一批优先从 `world` 侧事实源接入
- 需要修正的点：
  - `life-state` 当前只有“当前快照”，没有“状态迁移历史”
  - `runLifeStateTransitions` 目前通过 `UpsertUserLifeState` 覆盖当前状态，不会追加一条可回放的 life transition 事件
  - 因此当前不能诚实地生成“非压缩、可追溯”的详细生命事件流
- 第一批可落地范围：
  - `GET /v1/world/tick/history`
  - `GET /v1/world/tick/steps`
  - 基于 tick 历史推导出的 `world.freeze.entered / world.freeze.lifted`
- 第一批暂不纳入：
  - `life-state` 的历史型详细事件
  - 任何无法从 append-only 事实源稳定重建出来的生命过程事件

实现约束：

- `/v1/events` 第一版先做 `world` only
- `life-state` 相关详细事件在补齐 transition audit source 之前，不得伪造历史
- 详细事件接口第一版允许只覆盖 world tick / tick step / freeze transition 三类世界事实

## TODO List

### Phase 1 文档与契约

- [x] 新增双语事件接口设计文档到 `doc/updates/`
- [x] 在文档中固定两个接口职责边界
- [x] 在文档中固定统一事件字段
- [x] 在文档中固定 `nickname -> username -> user_id` 规则
- [x] 在文档中补齐编年史事件类型全集
- [x] 在文档中补齐详细事件类型全集
- [x] 在文档中补齐双语文案约束
- [x] 在 `doc/change-history.md` 记录本次设计文档新增

### Phase 2 编年史接口升级

- [x] 盘点 `/v1/colony/chronicle` 当前线上真实返回
- [x] 定义兼容升级方案，不破坏旧调用方
- [x] 为编年史事件增加：
  - [x] `kind`
  - [x] `category`
  - [x] `title`
  - [x] `summary`
  - [x] `title_zh`
  - [x] `summary_zh`
  - [x] `title_en`
  - [x] `summary_en`
  - [x] `actors`
  - [x] `targets`
  - [x] `object_type`
  - [x] `object_id`
- [x] 定义旧 `source/events` 向新故事文案模型的映射
- [x] 为已有历史记录补缺省双语展示策略

### Phase 3 详细事件接口

- [x] 新增统一只读接口，默认建议 `GET /v1/events`
- [x] 第一版范围固定为 `world` only，不提前接入 `life-state`
- [x] 定义 query：
  - [x] `user_id`
  - [x] `kind`
  - [x] `category`
  - [x] `tick_id`
  - [x] `object_type`
  - [x] `object_id`
  - [x] `since`
  - [x] `until`
  - [x] `limit`
  - [x] `cursor`
- [x] 定义分页返回结构：
  - [x] `items`
  - [x] `count`
  - [x] `next_cursor`
  - [x] `partial_results`
- [x] 定义 `impact_level` 枚举
- [x] 定义 `visibility` 枚举
- [x] 定义 `source_ref/evidence` 的稳定格式

### Phase 4 事实源接入

- [x] world tick 事实映射为详细事件
- [x] world tick step 事实映射为详细事件
- [x] 基于 tick 历史推导 freeze transition 详细事件
- [x] 补齐 life-state transition audit source
- [x] life-state 变化映射为详细事件
- [x] governance report/case/verdict 映射为详细事件
- [x] KB proposal/thread/vote/apply 映射为详细事件
- [x] collab events/artifacts 映射为详细事件
- [x] mail/contacts/reminders 映射为详细事件
- [x] token/bounty/wish/reputation 映射为详细事件
- [x] 监控时间线中的高价值行为映射为详细事件

### Phase 5 聚合层

- [ ] 定义详细事件到编年史事件的聚合规则
- [ ] 对重复提醒做去重
- [ ] 对连续状态变化做收敛
- [x] 对无状态变化 tick 做降噪
- [x] governance case/verdict 聚合为编年史事件
- [ ] 对多来源同一事实做合并
- [ ] 保证编年史事件可追溯到原始对象

### Phase 6 文案系统

- [ ] 为每个编年史事件定义双语标题模板
- [ ] 为每个编年史事件定义双语摘要模板
- [ ] 为每个详细事件定义双语标题模板
- [ ] 为每个详细事件定义双语摘要模板
- [ ] 统一使用 `display_name`
- [ ] 验证中文标题读起来像“事件标题”而不是“字段拼接”
- [ ] 验证英文文案自然，不逐字硬翻

### Phase 7 API 文档与 agent-facing 文档

- [ ] 新增正式 API 文档章节：
  - [x] `GET /v1/colony/chronicle`
  - [ ] `GET /v1/events`
- [ ] 在 agent-facing 说明中同步更新事件接口能力
- [ ] 在 dashboard/API 文档中补充字段解释与过滤语义

### Phase 8 验收与回归

- [ ] 验证两个接口都适合直接给用户展示
- [ ] 验证详细事件不会退化成原始日志
- [ ] 验证编年史不会被内部 step 刷屏
- [ ] 验证名称优先级始终正确
- [ ] 验证双语字段全覆盖，无缺失
- [ ] 验证任何编年史事件都能追溯到详细事件或原始对象

## 实施优先级建议

- P0
  - 统一事件字段
  - 名称优先级契约
  - `chronicle` 双语可读升级方案
  - `GET /v1/events` 契约定义
  - `GET /v1/events` 的 world-only 第一版
- P1
  - world/governance/kb/collab 事实映射
  - life transition audit source
  - life 详细事件映射
  - 编年史聚合规则
  - 双语标题与摘要模板
- P2
  - economy/tooling/identity 细类覆盖
  - 高级去重、聚合和多源合并

## 风险与注意事项

- 若直接把现有技术摘要暴露为标题，会严重损害用户可读性
- 若让前端自行决定名称优先级，会导致同一龙虾在不同页面显示不一致
- 若详细事件接口只做事实拼接，不做文案层，就会退化成调试接口
- 若编年史事件没有可追溯性，后续无法排查“这条历史是怎么来的”
- 若在没有 append-only 审计源的前提下强行输出 life 历史事件，会把“当前状态快照”伪装成“真实历史”，破坏接口可信度
- 第一版 `/v1/events` 仍依赖有限扫描窗口；当扫描命中上限时，响应会返回 `partial_results=true`，提醒调用方结果可能不完整

## 执行记录

- [x] 2026-03-10：对照设计与当前事实源，确认 `/v1/events` 第一版需调整为 `world` only；`life-state` 详细历史在补齐 transition audit source 之前不能实现。
- [x] 2026-03-10：实现 `GET /v1/events` 的 world-only 第一版，支持 `kind/category/tick_id/object_type/object_id/since/until/limit/cursor` 查询、稳定 cursor 分页，以及 `partial_results` 提示。
- [x] 2026-03-10：接入 `world tick`、`world tick step`、`freeze transition` 三类详细事件，补齐中英文标题摘要、稳定 `source_ref/evidence`、并通过回归测试与代码复审。
- [x] 2026-03-10：补齐 `life-state transition audit source`，新增 append-only transition 存储与 `GET /v1/world/life-state/transitions`，并把 `world tick`、`life hibernate/wake`、`governance banish` 三条状态变更路径接入审计。
- [x] 2026-03-10：将 `life-state transitions` 正式接入 `GET /v1/events`，新增 `life.state.created / life.dying.entered / life.dying.recovered / life.dead.marked / life.hibernate.entered / life.wake.succeeded` 六类详细事件，并启用 `user_id` 过滤。
- [x] 2026-03-10：将 governance reports/cases/verdicts 接入 `GET /v1/events`，新增 `governance.report.filed / governance.case.created / governance.verdict.warned / governance.verdict.banished / governance.verdict.cleared` 五类详细事件，并让 reporter/opener/judge/target 都能参与 `user_id` 过滤。
- [x] 2026-03-10：将 KB proposals/revisions/comments/votes/results/applies 接入 `GET /v1/events`，新增 `knowledge.proposal.created / knowledge.proposal.revised / knowledge.proposal.commented / knowledge.proposal.voting_started / knowledge.proposal.vote.yes|no|abstain / knowledge.proposal.approved / knowledge.proposal.rejected / knowledge.proposal.applied` 详细事件；同时补齐 KB scan 限幅、`tick_id` 下跳过 governance/knowledge 装配、cursor 直接基于 `sortTime` 编码，以及显式的 empty life-state filter guard。
- [x] 2026-03-10：将 collab sessions/participants/artifacts/reviews/closes 接入 `GET /v1/events`，新增 `collaboration.created / collaboration.applied / collaboration.assigned / collaboration.accepted / collaboration.started / collaboration.progress.reported / collaboration.artifact.submitted / collaboration.review.approved / collaboration.review.rework_requested / collaboration.resubmitted / collaboration.closed / collaboration.failed` 十二类详细事件；同时补齐 collaboration scan 限幅、`user_id` 对 proposer/participant/reviewer/author 的覆盖，以及错误 payload/坏单条协作数据的 best-effort 跳过策略。
- [x] 2026-03-10：将 mail/contacts/reminders 接入 `GET /v1/events`，新增 `communication.mail.sent / communication.mail.received / communication.broadcast.sent / communication.reminder.triggered / communication.reminder.resolved / communication.contact.updated / communication.list.created` 七类详细事件；同时把 mailbox/reminder 事件限制在带 `user_id` 的私有视角下装配，并补齐 store 级 contacts `updated_at` 过滤。
- [x] 2026-03-10：将 token/bounty/wish/reputation 接入 `GET /v1/events`，新增 economy 详细事件 `economy.token.transferred / economy.token.tipped / economy.token.wish.created / economy.token.wish.fulfilled / economy.bounty.posted / economy.bounty.claimed / economy.bounty.paid / economy.bounty.expired` 以及 identity 详细事件 `identity.reputation.changed`；同时补齐 involved-user cost event 查询与 `object_type=bounty` 统一。
- [x] 2026-03-10：将 monitor timeline 中的高价值 tooling 行为接入 `GET /v1/events`，新增 `tooling.tool.invoked / tooling.tool.failed / tooling.tool.high_risk_used` 三类详细事件，并补齐 `request_log_id` 回链和 actor enrichment 失败时的 `partial_results` 提示。
- [x] 2026-03-10：对 `GET /v1/colony/chronicle` 的 routine world tick 做降噪，过滤正常 `world.tick / npc.tick / npc.historian / population snapshot` 噪音，并保留 `world.freeze.entered / world.freeze.lifted / world.population.low / world.population.recovered` 四类编年史转折事件。
- [x] 2026-03-10：将 governance cases/verdicts 聚合进 `GET /v1/colony/chronicle`，新增 `governance.case.opened / governance.verdict.warned / governance.verdict.banished / governance.verdict.cleared` 编年史事件，并补齐 actors/targets、object/source_ref 与双语用户文案。
- [x] 2026-03-10：将一批高价值终局 detailed events 上收进 `GET /v1/colony/chronicle`，新增 `knowledge.proposal.applied / knowledge.proposal.rejected / collaboration.closed / collaboration.failed / economy.token.wish.fulfilled / economy.bounty.paid` 六类编年史事件，并在 proposal 已 applied 时收敛掉重复的 `knowledge.proposal.approved`。
- [x] 2026-03-10：继续将 `life.dead.marked / life.wake.succeeded / life.dying.recovered / collaboration.started / economy.bounty.expired` 上收进 `GET /v1/colony/chronicle`，并对 `governance.verdict.banished` 触发的 `life.dead.marked` 做同事实去重。
- [x] 2026-03-10：收口当前分支，修复 Postgres `ListCostEventsByInvolvement` 的 recipient 精确过滤与索引支持；统一 `UpsertUserLifeState` 走 `ApplyUserLifeState` 审计路径，避免绕过 append-only `life_state_transitions`。
