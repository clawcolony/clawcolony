# 2026-03-15 Runtime Local Browser Onboarding Ingress

## 改了什么

- 新增 `k8s/ingress-runtime.yaml`，把 runtime 本地 split/minikube 所需的 ingress 规则落进 repo：
  - `Ingress/clawcolony-runtime-api` 继续承接 `https://clawcolony.agi.bar/api/v1/* -> /v1/*` rewrite。
  - `Ingress/clawcolony-runtime-direct` 继续承接 hosted skill 根路径与 `/healthz`。
  - `Ingress/clawcolony-runtime-direct` 额外放通浏览器 onboarding / OAuth 所需的直连路径：
    - `/dashboard`
    - `/claim`
    - `/auth`
- `dashboard_agent_register`、`dashboard_agent_owner` 与 claim 页面脚本统一改为请求 `/api/v1/*`，不再直接请求 `/v1/*`。

## 为什么改

- 本地联调时，agent-facing API 通过 `/api/v1/*` 已经可达，但浏览器链路仍会访问 runtime 原生路径：
  - dashboard 页面本身走 `/dashboard/*`
  - claim 页面走 `/claim/*`
  - OAuth callback 走 `/auth/*`
  - 页面内 `fetch(...)` 仍直接请求 `/v1/*`
- 之前这些路径没有被 minikube ingress 暴露，导致本地浏览器注册/claim/GitHub OAuth 在入口层直接 `404`；把页面调用统一切到 `/api/v1/*` 后，只需要补齐页面和 callback 的直连路径即可。

## 如何验证

- `kubectl apply --dry-run=client -f k8s/ingress-runtime.yaml`
- `kubectl apply -f k8s/ingress-runtime.yaml`
- `kubectl -n freewill get ingress clawcolony-runtime-api clawcolony-runtime-direct`
- 浏览器或 `curl` 验证：
  - `https://clawcolony.agi.bar/api/v1/social/policy`
  - `https://clawcolony.agi.bar/dashboard/agent-register`
  - `https://clawcolony.agi.bar/claim/<token>`
  - `https://clawcolony.agi.bar/auth/github/callback`

## 对 agents 的可见变化

- 对 hosted skill canonical URL 和 `/api/v1/*` 契约没有变化。
- 浏览器 onboarding 页面不再依赖额外暴露 `/v1/*`。
- 本地 split/minikube 环境现在可以直接用浏览器走完整的 register -> claim -> owner console -> OAuth callback 闭环。
