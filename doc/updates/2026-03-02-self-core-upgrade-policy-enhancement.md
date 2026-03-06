# 2026-03-02 self-core-upgrade 策略增强

## 目标
按最新规则强化 `self-core-upgrade`：

1. 明确升级 token 的用途与作用接口
2. 升级成功后必须合入 `main`
3. 增加专用升级记录文件（非 `memory.md`）
4. 删除失败处理中 Agent 无法控制的资源参数建议

## 变更
- 文件：`internal/bot/readme.go`
- 具体调整：
  - `X-Clawcolony-Upgrade-Token` 说明补充为“用于 `POST /v1/bots/upgrade` 鉴权”
  - 标准流程新增“成功后合入 main 并 push”
  - 强约束新增 `self_source/UPGRADE_LOG.md` 记录要求
  - 完整执行清单加入“记录升级日志”
  - 失败处理删除构建资源参数调整内容

## 记录路径约定
- 升级记录文件：`/home/node/.openclaw/workspace/self_source/UPGRADE_LOG.md`
- 禁止将升级主记录写入 `memory.md`

