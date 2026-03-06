# 2026-03-05 严格 per-user git secret 模式（去全局）

## 背景
用户要求：

1. 避免重复出现“部署后才发现配置错位/全局凭据污染”的长等待问题。
2. 明确保证多用户场景下 `user_id`、`name`、`git secret` 三者独立。

## 本次调整

### 1) Deployer 改为严格 per-user（不再全局回退）
文件：`internal/bot/k8s_deployer.go`

- 移除 deploy 时对共享 secret 的兜底使用。
- 若找不到 `aibot-git-<user_id>`（或显式传入的 per-user secret），直接失败并报错。
- 增加 secret 存在性校验，避免“配置了但实际不存在”导致的隐式故障。
- `BOT_GIT_SSH_HOST` 的默认兜底改为 `github.com`。

### 2) 一键脚本默认做隔离验证
文件：`scripts/bootstrap_full_stack.sh`

- 新增参数：`--skip-verify-isolation`。
- 默认在 register 阶段完成后自动执行：
  - `scripts/check_agent_isolation.sh --namespace freewill --use-minikube auto`
- 目标：立即发现并阻断“重复 user/name/secret”与“全局 secret 混入”。

### 3) 文档与脚本去除全局 SSH secret 入口
文件：

- `scripts/oneclick.env.example`
- `scripts/deploy_dev_server.sh`
- `README.md`
- `doc/updates/2026-03-05-agent-isolation-and-remote-fast-fail.md`

变更点：

- 移除 `GLOBAL_GIT_SSH_*` 相关引导。
- `deploy_dev_server.sh` 改为提示“per-user secret 在 register 中自动创建”。
- README 改为 per-user 模式说明，并明确可用隔离检查脚本验证。

## 结果

- 运行链路中不会再因共享 secret 产生跨用户污染。
- register 完成后可自动触发独立性校验，减少后置排查时间。
- 文档、脚本、运行逻辑已统一为 per-user 模式。
