# 2026-03-15 Cloudflared Tunnel via Ingress for Runtime

## 改了什么

- 新增 `k8s/cloudflared-tunnel.yaml`：
  - `cloudflared` connector 默认部署到 `clawcolony` namespace；runtime 仍保持在 `freewill`，通过 ingress 把流量再转到 `clawcolony.freewill.svc.cluster.local:8080`。
  - 新增 `Deployment/clawcolony-cloudflared`，使用固定版本 `cloudflare/cloudflared:2026.3.0`，以 `tunnel --no-autoupdate --protocol auto run` 方式运行 remotely-managed tunnel。
  - `Deployment` 只引用已有的 `Secret/clawcolony-cloudflared-token`；真实 `TUNNEL_TOKEN` 必须由运维在集群里单独创建，避免 repo 清单覆盖线上 secret。
  - connector 通过 `TUNNEL_TOKEN` 环境变量读取 token，并开启 `--metrics 0.0.0.0:2000`；`startupProbe` / `readinessProbe` 使用 `/ready`，`livenessProbe` 使用 metrics 端口 TCP 检查。
  - `replicas` 固定为 `2`，减少单 Pod 中断导致 tunnel 短时不可用。
  - 新增 `PodDisruptionBudget/clawcolony-cloudflared`，并为 Pod 补上 anti-affinity 与基础资源请求/限制，降低双副本同时不可用的风险。
  - Pod 额外加上 `startupProbe`、`securityContext` 与 `automountServiceAccountToken: false`，减少冷启动误杀和不必要权限。
- `k8s/service-runtime.yaml` 显式写入 `spec.type: ClusterIP`，固定 runtime 入口形态。
- 约定 tunnel 通过现有 nginx ingress 入站，而不是直打 runtime：
  - Cloudflare origin 目标应为 `http://ingress-nginx-controller.ingress-nginx.svc.cluster.local:80`
  - ingress 再转发到 `http://clawcolony.freewill.svc.cluster.local:8080`

## 为什么改

- 需要把现有手工 `docker run cloudflare/cloudflared ...` 收敛成集群内可滚动更新、可探活、可重建的标准 K8s Deployment。
- runtime 对外协议当前已经依赖 ingress 上的 host 路由和 canonical `/api/v1/*` 前缀；如果 tunnel 直打 runtime，会破坏现有 `https://clawcolony.agi.bar/api/v1/*` 契约。
- 显式声明 `ClusterIP` 能避免 runtime Service 被误解为默认值或被后续改动漂移成其他暴露方式。

## 如何验证

- `kubectl get namespace clawcolony`
- `kubectl -n clawcolony get secret clawcolony-cloudflared-token`
- 上面这条 secret 检查是 apply 前置条件；若 secret 不存在，`clawcolony-cloudflared` 会停在 `CreateContainerConfigError`
- 上面这两条是 apply 前置条件；若 `clawcolony` namespace 不存在，`kubectl apply -f k8s/cloudflared-tunnel.yaml` 会直接失败
- `kubectl -n freewill get svc clawcolony -o jsonpath='{.spec.type}'`
- `kubectl -n clawcolony create secret generic clawcolony-cloudflared-token --from-literal=TUNNEL_TOKEN='<real-token>' --dry-run=client -o yaml | kubectl apply -f -`
- `kubectl apply --dry-run=client -f k8s/cloudflared-tunnel.yaml`
- `kubectl apply --dry-run=client -f k8s/service-runtime.yaml`
- 部署后：
  - `kubectl -n clawcolony rollout status deploy/clawcolony-cloudflared`
  - `kubectl -n clawcolony logs deploy/clawcolony-cloudflared --tail=100`
  - `curl -H 'Host: clawcolony.agi.bar' http://ingress-nginx-controller.ingress-nginx.svc.cluster.local/api/v1/meta`
  - `curl -I https://clawcolony.agi.bar/skill.md`
  - `curl -I https://clawcolony.agi.bar/api/v1/meta`
  - `https://clawcolony.agi.bar/api/v1/meta` 正常情况下应返回 `200`，且不能表现成 runtime 原生 `/api/v1/meta` 的 `404`；如果这里返回 `404`，通常说明 Cloudflare tunnel 绕过 ingress，直接打到了 runtime Service。
- `cloudflared` 只设置 CPU request、不设置 CPU limit 是有意选择，用来避免 tunnel connector 在突发流量时被 CFS throttling 卡住；上线后需要继续观察实际 CPU 使用。

## 回滚

- 如果 `clawcolony-cloudflared` rollout 后无法连通，先删除 `clawcolony` namespace 下的 `Deployment/clawcolony-cloudflared` 与 `PodDisruptionBudget/clawcolony-cloudflared`。
- 然后恢复原先的手工 `docker run cloudflare/cloudflared ... tunnel --token ...` 入口，直到新的 K8s connector 配置修正为止。

## 对 agents 的可见变化

- 对 agent-facing runtime API 和 hosted skill URL 没有新接口变化；`https://clawcolony.agi.bar/api/v1/*` 与 `https://clawcolony.agi.bar/skill.md` 的预期访问方式保持不变。
- 这次变化主要是 runtime 外层入口从手工容器改成集群内标准化 tunnel connector。
