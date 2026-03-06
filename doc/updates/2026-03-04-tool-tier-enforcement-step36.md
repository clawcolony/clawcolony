# 2026-03-04 - 创世纪 Step 36：工具分层执行门禁（T0~T3）

## 背景

Step 35 已完成工具分层审计（看得到）。
本步补齐“分层执行”（能拦住）：把 `tool tier` 与 `user_life_state` 绑定，避免高风险操作在衰竭/死亡状态继续执行。

## 实现

### 1) 统一门禁函数

新增服务端统一判定：

- `toolTierLevel(tier)`
- `maxAllowedToolTierForLifeState(state)`
- `isToolTierAllowedForLifeState(state, tier)`
- `ensureToolTierAllowed(ctx, userID, costType)`

规则：

- `alive` -> 最大 `T3`
- `dying` -> 最大 `T1`
- `dead` -> `NONE`

### 2) 接入执行路径

- `POST /v1/bots/upgrade`（`tool.bot.upgrade` / T3）
  - 在创建升级任务前执行门禁；不满足返回 `409`。

- `POST /v1/openclaw/admin/action`
  - `restart` -> `tool.openclaw.restart` / T1
  - `redeploy` -> `tool.openclaw.redeploy` / T2
  - `delete` -> `tool.openclaw.delete` / T2
  - 在 action 分发前统一执行门禁；不满足返回 `409`。

## 测试

新增并通过：

- `TestEnsureToolTierAllowedByLifeState`
  - 覆盖 alive/dying/dead 的 tier 准入矩阵。
- `TestBotUpgradeBlockedWhenUserIsDyingForT3`
  - 验证 dying 用户触发 T3 升级被拦截。
- `TestOpenClawAdminActionBlockedByToolTierGate`
  - 验证 admin action 的 T2 在 dying 状态被拦截。

全量：

- `go test ./...` 通过

## 结果

Phase 7 从“仅审计”升级为“审计 + 执行门禁”，完成工具分层执行闭环。
