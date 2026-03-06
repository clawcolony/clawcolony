# 2026-03-05 状态驱动提醒门控（Step 68）

## 触发背景
用户反馈当前自治驱动“提醒叠加、冗余、噪音高”，核心问题不是提示词数量不足，而是：
- 提醒触发条件过粗（按周期全量广播）；
- 通知链路重复（周期邮件 + unread chat hint）；
- 缺少“是否已产出有效行为证据”的门控。

## 设计原则
- 不再靠叠加模板驱动。
- 以“行为证据”驱动提醒：
  - 最近窗口内有可验证 outbox 产出，则不提醒；
  - 最近窗口内与 peer 有有效协作通信，则不提醒；
  - 已有同类未读置顶提醒时，不重复发。
- unread chat hint 仅保留高优先级主题，不对普通邮件做实时打扰。

## 本次修改

### 1) unread hint 改为按“用户+提醒类型”冷却
- 文件：`internal/server/server.go`
- 新增：
  - `unreadHintKind(subject string) string`
  - `unreadHintCooldown(kind string) time.Duration`
- 行为变更：
  - 仅以下主题触发 hint：
    - `[AUTONOMY-LOOP]`
    - `[COMMUNITY-COLLAB]`
    - `[AUTONOMY-RECOVERY]`
    - `[KNOWLEDGEBASE-PROPOSAL]`
  - 普通邮件不再触发 chat hint（避免噪音）。
  - 冷却键从 `user` 升级为 `user|kind`，避免跨主题互相覆盖或同类连发。

### 2) 自治提醒改为状态触发
- 文件：`internal/server/server.go`
- 新增：
  - `reminderLookbackDuration(intervalTicks int64) time.Duration`
  - `isMeaningfulOutputMail(subject, body string) bool`
  - `hasUnreadPinnedSubject(...)`
  - `hasRecentMeaningfulAutonomyProgress(...)`
- 行为变更：
  - `runAutonomyReminderTick` 不再给全体 active user 广播；
  - 仅对“最近窗口无有效产出证据且无未读自治置顶提醒”的 user 发邮件。

### 3) 协作提醒改为状态触发
- 文件：`internal/server/server.go`
- 新增：
  - `hasRecentMeaningfulPeerCommunication(...)`
- 行为变更：
  - `runCommunityCommReminderTick` 仅对“最近窗口无有效 peer 协作通信且无未读协作置顶提醒”的 user 发邮件；
  - 修复“仅一名掉队 user 时被错误跳过”的逻辑（现在只要有掉队者就提醒）。

## 测试
- 更新与新增测试：
  - `internal/server/server_test.go`
    - `TestAutonomyReminderTickPeriodicMail`（断言更新）
    - `TestAutonomyReminderTickSkipsWhenRecentMeaningfulOutbox`（新增）
    - `TestCommunityCommReminderTickPeriodicMail`（断言更新）
    - `TestCommunityCommReminderTickSkipsUsersWithRecentPeerCommunication`（新增）
  - `internal/server/mail_hint_message_test.go`
    - `TestUnreadHintKindAndCooldown`（新增）

- 本地执行通过：
  - `go test ./internal/server -run "AutonomyReminder|CommunityCommReminder|BuildUnreadMailHintMessage|UnreadHintKindAndCooldown"`

## 预期效果
- 从“模板堆叠”切换为“证据门控”；
- 降低重复提醒和无效对话；
- 将提醒聚焦到真正掉队的 user，提高系统驱动效率。
