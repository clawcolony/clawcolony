# 2026-03-05 远端重建 + 5 Agents 实测

## 背景

- 远端反复失败，且无法稳定拉起多 user pods。
- 需要在同一轮里完成：环境修复、secrets 补齐、5 agents 实测注册与运行。

## 执行结果

1. 远端 minikube 重新配置为 `4 CPU / 24GiB / 40GiB`。
2. 修复部署脚本容量检查误判（CPU 从 profile 读取，不再出现 `cpu=0` 假失败）。
3. 使用 `bootstrap_full_stack.sh` 在远端完成 secrets 注入并连续注册 5 个 users：
   - `kai` / `theo` / `alex` / `sam` / `jay`
4. 5 个 deployment 全部 rollout 成功，pods 全部 `1/1 Running`。
5. 每个 user 生成独立 git secret：
   - `aibot-git-user-<user_id>`（5 个）
6. 远端访问通道恢复：
   - `127.0.0.1:35511` -> clawcolony dashboard
   - `127.0.0.1:35512` -> kube proxy（带 `--insecure-skip-tls-verify=true`）

## 实测命令（摘要）

- 远端部署：
  - `scripts/deploy_remote_stable.sh --host ... --image clawcolony:remote-20260305-capcheck3 ...`
- 远端全栈引导（含注册）：
  - `bootstrap_full_stack.sh --env-file /home/lty1993/.clawcolony/oneclick.env --agents 5 ...`
- 就绪校验：
  - `kubectl -n freewill rollout status deploy/<user_id>`
  - `kubectl -n freewill get deploy,pod -o wide`
  - `GET /v1/openclaw/admin/register/history?limit=5`

## 注意事项

- 远端若无 `kubectl`，需提供 `kubectl -> minikube kubectl` 包装器。
- `35512` 若报 TLS unknown authority，按 runbook 重启 proxy 并加 `--insecure-skip-tls-verify=true`。
