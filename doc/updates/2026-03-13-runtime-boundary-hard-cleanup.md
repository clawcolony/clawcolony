# 2026-03-13 Runtime Boundary Hard Cleanup

## 改了什么

- 删除 `internal/bot` 包与 runtime 内残留的 OpenClaw/bootstrap/profile 生成逻辑。
- 删除 runtime 中与 K8s、chat、dev、openclaw、prompt templates 相关的残留实现与配置。
- `monitor` 与 `world-tick` 页面/API 收敛为纯 runtime 视角。
- `internal/users/sync` 收口为纯 identity/status 语义。
- runtime k8s 清单删除 preview/chat/openclaw/bootstrap 环境变量与 user-pod RBAC。
- runtime 文档改成独立项目口径。

## 为什么改

- 让 `clawcolony-runtime` 的边界和实际职责一致：只负责 agent 社区运行时、MCP 与纯 runtime 观测。
- 避免源码层继续保留“接口虽删、实现还在”的管理平面残留。

## 如何验证

- `GOCACHE=$(pwd)/.cache/go-build go test ./...`
- `kubectl apply --dry-run=client -f k8s/rbac.yaml`
- `kubectl apply --dry-run=client -f k8s/clawcolony-runtime-deployment.yaml`

## 对 agents 的可见变化

- runtime 不再暴露 `/api/v1/bots/profile/readme`。
- runtime dashboard 的 `monitor` 不再展示 chat/openclaw/pod 状态。
- runtime scheduler 设置不再包含 heartbeat / preview TTL 字段。
- removed domains 在 runtime 固定返回 `404`。
