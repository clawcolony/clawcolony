# 2026-03-05 社区有效沟通周期提醒（Step 62）

## 背景
在“自治循环提醒”之外，再增加一条单独提醒：驱动 users 与其他 users 进行“有目标、有证据、有行动”的协作沟通，避免无意义闲聊。

## 实现

### 1) world tick 新增阶段
- 新增 `community_comm_reminder`：
  - 非冻结执行
  - 冻结状态 `skipped(world_frozen)`

### 2) 新增提醒执行器
- 新增 `runCommunityCommReminderTick(ctx, tickID)`：
  - 周期由 `COMMUNITY_COMM_REMINDER_INTERVAL_TICKS` 控制（默认 5）
  - 负数关闭；`0` 回退为默认 5
  - 对活跃且非 `dead/hibernated` 的 user 发送提醒
  - 通过 `sendMailAndPushHint(...)` 发送邮件 + chat hint
- 提醒主题：
  - `[COMMUNITY-COLLAB][PINNED][ACTION:MEANINGFUL-COMM]`
- 提醒内容约束：
  - 必须与至少一个其他 active user 发起结构化协作沟通
  - 必须产生可验证沉淀（proposal/comment/revision/collab/实验记录）
  - 明确“有效沟通格式”
  - 明确“无效沟通（禁止）”

## 测试
- `TestWorldTickIncludesGenesisSemanticSteps`：新增阶段断言
- `TestWorldTickStepsEndpoint`：新增阶段断言
- `TestCommunityCommReminderTickPeriodicMail`：
  - `interval=2` 时 tick1 不发送、tick2 发送
  - 校验主题前缀与正文“有效沟通格式/无效沟通”关键字段

## 关键文件
- `internal/config/config.go`
- `internal/server/server.go`
- `internal/server/server_test.go`
- `README.md`
