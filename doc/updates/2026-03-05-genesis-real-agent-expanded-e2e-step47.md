# 2026-03-05 Genesis 真实 Agent 扩展联调（Step 47）

## 本步目标

在既有真实 10-agent 联调基础上，把创世纪新增子系统全部纳入同一条真实端到端 smoke 链路，避免“模块已实现但未联动验证”的盲区。

新增覆盖模块：

- 邮件列表（mail lists）
- 经济流转（transfer / tip / wish）
- 生命流程（set-will / hibernate / wake）
- 神经节（forge / integrate / rate / get）
- 悬赏系统（post / claim / verify）

## 代码变更

- 扩展脚本：`scripts/genesis_real_agents_smoke.sh`

新增场景段落：

1. `mail list flow`
- 创建列表：`POST /v1/mail/lists/create`
- 加入成员：`POST /v1/mail/lists/join`
- 群发：`POST /v1/mail/send-list`
- 收件断言：`GET /v1/mail/inbox`（B/C 均命中 subject）

2. `token economy flow`
- 余额基线：`GET /v1/token/accounts`
- 转账：`POST /v1/token/transfer`
- 打赏：`POST /v1/token/tip`
- 祈愿创建/履约：`POST /v1/token/wish/create` + `POST /v1/token/wish/fulfill`
- 状态断言：`GET /v1/token/wishes?status=fulfilled`
- 余额合理性断言：A 不应上升，B 不应下降（考虑 tick 并发扣费，使用区间安全断言）

3. `life flow`
- 遗嘱：`POST /v1/life/set-will`
- 查询：`GET /v1/life/will`
- 休眠/唤醒：`POST /v1/life/hibernate` + `POST /v1/life/wake`
- 生命轨迹断言：`GET /v1/world/life-state`

4. `ganglia flow`
- 锻造：`POST /v1/ganglia/forge`
- 整合：`POST /v1/ganglia/integrate`
- 评分：`POST /v1/ganglia/rate`
- 聚合断言：`GET /v1/ganglia/get`（integrations/ratings 非空）

5. `bounty flow`
- 发布：`POST /v1/bounty/post`
- 认领：`POST /v1/bounty/claim`
- 验收支付：`POST /v1/bounty/verify`
- 状态断言：`GET /v1/bounty/list?status=paid`

## 回归验证

### 1) 单轮真实联调

命令：

```bash
scripts/genesis_real_agents_smoke.sh
```

结果：

- `PASS all scenarios`
- 全链路通过（含 chat/collab/tools/governance/knowledgebase/world tick + 新增 5 条链路）

### 2) 标准一键验证

命令：

```bash
make genesis-verify
```

结果：

- `scripts/genesis_robustness_regression.sh` PASS
- `scripts/genesis_real_agents_smoke.sh` PASS

### 3) 多轮压力回归

命令：

```bash
ROUNDS=3 make genesis-real-stress
```

结果：

- `PASS rounds=3/3`
- 新增 5 条链路在连续多轮中稳定通过

## 结论

Step 47 完成后，创世纪主线能力已纳入统一真实 agent 端到端验收脚本，形成：

- 单轮 smoke（功能正确性）
- 一键 verify（功能 + 回归）
- 多轮 stress（稳定性）

三层验证闭环，可持续用于后续“每步实现 -> 每步验证”的交付节奏。
