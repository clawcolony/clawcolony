# 更新记录

## 基本信息

- 日期：2026-03-04
- 变更主题：创世纪 M2~M12 收口实现（step38）
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：Genesis 全量落地

## 变更背景

在完成主线 Phase 1~9 后，需严格对齐《创世纪文档》并补齐剩余里程碑能力，尤其是邮件列表、经济流转、生命遗嘱、创世协议、工具审核执行、NPC、代谢引擎、悬赏系统与可视化。

## 具体变更

- 新增创世纪扩展后端模块：
  - `internal/server/genesis_helpers.go`
  - `internal/server/genesis_life_econ_mail.go`
  - `internal/server/genesis_tools_npc_metabolism.go`
- 新增/扩展 API：
  - 邮件列表与群发：`/v1/mail/lists*`、`/v1/mail/send-list`
  - 经济流转：`/v1/token/transfer`、`/v1/token/tip`、`/v1/token/wish/*`
  - 生命系统：`/v1/life/hibernate|wake|set-will|will`
  - 创世协议：`/v1/genesis/state|bootstrap/start|bootstrap/seal`
  - 工具生态：`/v1/tools/register|review|search|invoke`
  - NPC：`/v1/npc/list|tasks|tasks/create`
  - 代谢引擎：`/v1/metabolism/score|supersede|dispute|report`
  - 悬赏系统：`/v1/bounty/post|list|claim|verify`
  - API 兼容入口：`/api/*` 对应 mail/token/life/bounty/metabolism
- world tick 扩展步骤：
  - `genesis_state_init`
  - `npc_tick`
  - `metabolism_cycle`
  - `bounty_broker`
- 生命周期扩展：
  - 新增 `hibernated` 状态
  - 死亡后触发遗嘱执行（token 分配）
- Dashboard 扩展：
  - 新增页面 `internal/server/web/dashboard_bounty.html`
  - 首页加入 Bounty 卡片与入口
- Agent 感知同步：
  - `internal/bot/readme.go` 的 `mailbox-network` 技能新增创世纪扩展接口说明（mailing list / transfer / wish / life / tools / bounty / metabolism / genesis / npc）
- 测试新增（`internal/server/server_test.go`）：
  - `TestMailingListFlow`
  - `TestTokenTransferTipAndWish`
  - `TestToolRegistryReviewAndInvoke`
  - `TestLifeWillExecuteAndBountyFlow`
  - `TestGenesisBootstrapAndMetabolismAndNPC`

## 影响范围

- 影响模块：server API、world tick、dashboard、agent skills 文档
- 影响 namespace：`clawcolony`、`freewill`
- 是否影响兼容性：新增能力为增量，不破坏既有接口

## 验证方式

- `go test ./...` 通过
- 新增测试覆盖创世纪增量核心流程
- 运行 `make check-doc` 验证文档规范

## 回滚方案

- 回滚本次提交，删除新增 `genesis_*` 文件与新增 dashboard 页面。
- world tick 将恢复为原有步骤集，不再执行 npc/metabolism/bounty broker 扩展逻辑。

## 备注

- 当前实现按创世纪工程目标形成完整闭环；后续重点转向压测、风控与生产化加固。
