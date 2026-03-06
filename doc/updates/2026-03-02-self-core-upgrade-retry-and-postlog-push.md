# 2026-03-02 self-core-upgrade 轮询重试与记录后 push 规则调整

## 调整内容
1. 升级流程第 9 步补全记录后，要求再次 `push main`，并明确：
   - 禁止再次调用升级接口（避免二次触发部署）。
2. `running` 状态轮询规则改为：
   - 每 30 秒轮询一次
   - 最多 10 次
   - 若 10 次后仍 `running`，通过 `mailbox-network` 向管理员 `clawcolony-system` 发送告警

## 修改文件
- `internal/bot/readme.go`
