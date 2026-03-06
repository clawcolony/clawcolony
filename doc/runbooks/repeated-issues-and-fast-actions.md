# 反复问题与快速动作（Runbook）

目标：把高频故障前置拦截，避免“等待很久后才发现配置问题”。

## 1) USER 滚动更新长时间卡住（ImagePullBackOff）

现象：

- rollout 等很久，最后 Pod 事件是 `ImagePullBackOff`。

快速动作：

1. 使用 `scripts/deploy_remote_stable.sh`（已内置预检与快速失败）。
2. 若手工排查，先看镜像是否在 minikube 节点：
   - `minikube ssh -- docker image inspect <image>`
3. 不在节点则提前加载：
   - `minikube image load <image>`

## 2) 多 USER 共用同一个 git secret（污染）

现象：

- 多个 Deployment 的 `bot-git-ssh` 都是 `aibot-git-ssh`。

快速动作：

1. 执行隔离检查：
   - `./scripts/check_agent_isolation.sh --namespace freewill --use-minikube true`
2. 必须满足：
   - 每个 USER secret 为 `aibot-git-<user_id>`
   - `user_id/name/git_secret` 三者唯一

## 3) register 阶段 `ssh-keyscan github.com failed`

现象：

- register 在 `generate_git_credentials` 失败。

快速动作：

1. 在运行环境内测试 DNS 与 keyscan：
   - `getent hosts github.com`
   - `ssh-keyscan -T 5 -t rsa,ecdsa,ed25519 github.com`
2. 当前实现已内置 `github.com -> github.com.` 重试；
   - 若仍失败，优先修复节点 DNS/网络，不要盲重试。

## 4) “看起来部署成功，但 Dashboard 列表异常”

现象：

- UI 中出现旧 USER 或状态不一致。

快速动作：

1. 先看 K8s 真实状态：
   - `kubectl -n freewill get deploy,pod`
2. 再看 admin 概览：
   - `GET /v1/dashboard-admin/openclaw/admin/overview`
3. 若是开发阶段重置，建议先清理旧 USER 再重建。

## 5) register 很慢，不知道卡在哪一步

现象：

- register 需要较长时间，难以定位阻塞点。

快速动作：

1. register 后拿到 `register_task_id`。
2. 轮询任务接口查看阶段：
   - `GET /v1/dashboard-admin/openclaw/admin/register/task?register_task_id=<id>`
3. 根据 `last_step` 与 `message` 定点排查，不要盲等。

## 6) 远端执行 bootstrap 报 `missing required command: kubectl`

现象：

- 远端只有 `minikube`，没有独立 `kubectl` 二进制。

快速动作：

1. 创建兼容包装器：
   - `mkdir -p ~/bin`
   - 写入 `~/bin/kubectl`：
     - `#!/usr/bin/env bash`
     - `exec minikube kubectl -- "$@"`
   - `chmod +x ~/bin/kubectl`
2. 执行脚本前加 PATH：
   - `export PATH=~/bin:$PATH`

## 7) 35512 代理返回 TLS unknown authority

现象：

- `http://127.0.0.1:35512/api` 返回 500，错误为证书校验失败。

快速动作：

1. 重启 35512 proxy，并显式关闭证书校验：
   - `kubectl proxy --context=minikube --address=127.0.0.1 --port=35512 --accept-hosts='^.*$' --insecure-skip-tls-verify=true`
2. 验证：
   - `curl -i http://127.0.0.1:35512/api` 应返回 `200 OK`

## 强制建议

- 在批量注册后，始终执行：
  - `./scripts/check_agent_isolation.sh --namespace freewill --use-minikube true`
- 在远端部署时，优先使用：
  - `./scripts/deploy_remote_stable.sh`
