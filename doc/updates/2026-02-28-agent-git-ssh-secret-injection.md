# 2026-02-28 Agent Git SSH Secret 注入

## 背景

为支持 OpenClaw 在 Pod 内直接提交并推送代码，需要为 USER Pod 提供可选的 Git SSH 凭据注入能力。

## 本次变更

1. 新增配置项
- `BOT_GIT_SSH_SECRET_NAME`：USER Pod 使用的 SSH Secret 名称（为空则不注入）
- `BOT_GIT_SSH_HOST`：Git 主机名（默认 `gitlab.webpilotai.com`）

2. USER Deployment 注入逻辑
- 当 `BOT_GIT_SSH_SECRET_NAME` 非空时：
  - 挂载 Secret 到 `/etc/clawcolony/git`
  - 要求 Secret keys：
    - `id_ed25519`
    - `known_hosts`
  - 注入环境变量：
    - `GIT_SSH_COMMAND=ssh -i /etc/clawcolony/git/id_ed25519 -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/clawcolony/git/known_hosts`
    - `GIT_SSH_VARIANT=ssh`
    - `CLAWCOLONY_GIT_HOST`

3. 部署清单
- `k8s/clawcolony-deployment.yaml` 默认增加：
  - `BOT_GIT_SSH_SECRET_NAME=aibot-git-ssh`
  - `BOT_GIT_SSH_HOST=gitlab.webpilotai.com`

## 影响

- 新注册/重建的 USER Pod 可在容器内直接执行 `git push`。
- 未配置 `BOT_GIT_SSH_SECRET_NAME` 时保持旧行为，不影响现有流程。

## 回滚

- 将 `BOT_GIT_SSH_SECRET_NAME` 置空并重新部署 Clawcolony。
