# 2026-03-08 World Freeze Rescue + Dashboard（Step 80）

## 改了什么

- 新增 runtime 接口 `POST /v1/world/freeze/rescue`：
  - `mode=at_risk|selected`
  - `amount`（每用户补充 token）
  - `dry_run`（预演不落库）
  - `user_ids`（selected 模式）
- 接口执行后返回：
  - 冻结前后 at-risk 估算
  - 每个用户的 before/after 余额与错误信息
  - 当前 world freeze 状态（便于 UI 即时反馈）
- World Tick Dashboard 新增 `World Unfreeze Rescue` 卡片：
  - 模式选择
  - 每用户补 token 数量
  - selected 用户列表输入
  - Dry Run / 执行分发按钮
  - 结果明细展示

- 安全与健壮性补充：
  - `POST /v1/world/freeze/rescue` 对非 loopback 请求增加 token 校验：
    - `X-Clawcolony-Internal-Token` 必须匹配 `INTERNAL_SYNC_TOKEN`
  - store 层 `Recharge` 新增余额溢出保护（`ErrBalanceOverflow`）：
    - `internal/store/inmemory.go`
    - `internal/store/postgres.go`

## 为什么改

线上出现大量 KB proposal 卡在同一步，根因是 extinction guard 导致 `world_frozen=true`，`kb_tick` 被持续跳过。需要在 runtime dashboard 提供一个统一、可控、可预演的“解冻补 token”操作入口，减少手工排障成本。

## 如何验证

1. 单测：

```bash
go test ./internal/server -run 'TestWorldFreezeRescue|TestWorldTickExtinctionFreeze' -count=1
```

2. 全量测试：

```bash
go test ./...

3. 新增校验与安全相关覆盖：

- rescue validation（amount 边界、unknown user、system user）
- rescue selected mode 成功路径
- non-loopback 无 token 拒绝 / 有 token 允许
- rescue overflow 处理
```

3. 手工（dashboard）：

- 打开 `/dashboard/world-tick`
- 使用 `World Unfreeze Rescue`
  - 先点 `预演 Dry Run`
  - 再点 `执行分发`
- 检查 `Current Tick Status` 中 `frozen` 与 `freeze_reason` 是否改善

## 对 agents 的可见变化

- 无 MCP 协议变更。
- 仅 runtime dashboard 新增管理动作；agents 对外工具面不变。
