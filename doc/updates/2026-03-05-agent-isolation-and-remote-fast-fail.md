# 2026-03-05 Agent 独立凭据 + 远端部署快速失败改进

## 背景
反复出现两类低效问题：

1. 远端 minikube 重启后，本地标签镜像不在节点内，滚动更新长时间卡在 `ImagePullBackOff`。
2. 部分 USER Deployment 使用了全局 `aibot-git-ssh`，与“每个 agent 独立 git secret”目标不一致。

## 本次改动

### 1) Register 流程强制每用户独立 git secret
文件：`internal/server/openclaw_admin.go`

- 去除 mock 场景下回退全局 `BOT_GIT_SSH_SECRET_NAME` 的逻辑。
- register 流程统一强制：
  - 生成每用户 SSH 凭据（`aibot-git-<user_id>`）
  - 创建 deploy key
  - 写入 per-user Secret
- 失败策略改为快速失败，不再静默降级到全局 secret。

### 2) SSH known_hosts 生成改为“短超时 + 重试”
文件：`internal/server/openclaw_admin.go`

- `ssh-keyscan` 增加超时：`-T 5`。
- 增加一次 FQDN 重试：`github.com` -> `github.com.`。
- 仍失败则明确报错，避免长时间挂起。

### 3) Deployer 优先保留并复用每用户独立源配置
文件：`internal/bot/k8s_deployer.go`

- Deploy 时新增保留策略（按优先级）：
  1. 本次 spec 显式传入
  2. 读取现有 Deployment 中的 `CLAWCOLONY_SOURCE_REPO_URL/BRANCH` 与 `bot-git-ssh` secret
  3. 自动尝试 `aibot-git-<user_id>`（若 secret 存在）
  4. 若仍无可用 secret，直接失败（不再回退到全局）
- 目的：防止运行中 `apply profile/redeploy` 把 per-user 配置覆盖回全局，并强制隔离。

### 4) 远端部署脚本加入预检与快速失败
文件：`scripts/deploy_remote_stable.sh`

新增：

- minikube 节点健康快照（内存/磁盘）
- USER 部署镜像预检：
  - 本地标签镜像若不在 minikube，会自动尝试 `minikube image load`
  - 若 host 与 minikube 都不存在该镜像，直接失败退出
- 目标：避免 rollout 长时间盲等后才发现 `ImagePullBackOff`。

### 5) 增加 agent 独立性校验脚本
文件：`scripts/check_agent_isolation.sh`

校验内容：

- user_id 唯一
- readable name 唯一
- git secret 非空
- 禁止全局 `aibot-git-ssh`
- git secret 必须匹配 `aibot-git-<user_id>`

## 配置默认值修正

- `internal/config/config.go`：`BOT_GIT_SSH_HOST` 默认值改为 `github.com`。
- `k8s/clawcolony-deployment.yaml`：
  - `BOT_GIT_SSH_SECRET_NAME` 默认改为空（推荐 per-user）
  - `BOT_GIT_SSH_HOST` 改为 `github.com`
  - `UPGRADE_REPO_URL` 默认置空，避免误导到历史 GitLab 地址。
- `scripts/oneclick.env.example`：同步改为 per-user secret 模式说明。

## 操作建议

1. 部署前先跑：
   - `scripts/deploy_remote_stable.sh`（内置预检）
2. 部署后必跑：
   - `scripts/check_agent_isolation.sh --namespace freewill --use-minikube true`
3. 若发现全局 secret 仍被引用：
   - 触发一次按 user 的 profile re-apply 或重建该 user deployment。

## 验证

- `go test ./internal/bot ./internal/server`
- `bash -n scripts/deploy_remote_stable.sh`
- `bash -n scripts/check_agent_isolation.sh`
