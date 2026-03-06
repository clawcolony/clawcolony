# 2026-03-04 - Tick 生存扣费改为读取天道 LIFE_COST_PER_TICK

## 背景

统一 World Tick 后，生存扣费仍使用硬编码常量。为对齐《创世纪》天道参数，应由天道参数驱动生存成本。

## 具体变更

1. `runTokenDrainTick` 改为使用 `cfg.LifeCostPerTick`。
2. 当配置值无效（<=0）时，回退到兼容常量默认值。
3. 新增测试 `TestTokenDrainUsesTianDaoLifeCost`：
- 设置 `LifeCostPerTick=3`
- 用户初始 10 token
- 执行一次 tick 后验证余额为 7

## 影响范围

- 影响文件：`internal/server/server.go`、`internal/server/server_test.go`
- 无 API 兼容性破坏。

## 验证方式

1. `go test ./...`
2. 查看 token 账户变化是否匹配配置值。

## 回滚说明

- 回滚该变更后，恢复固定生存扣费常量。
