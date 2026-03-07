# 2026-03-07 Runtime 单实例收敛与地址迁移防回归（Step 77）

## 改动摘要

- `scripts/dev_minikube.sh` 增加了运行时防回归保护：
  - 支持在无 `kubectl` 的环境自动回退到 `minikube kubectl --`
  - 部署后自动清理 legacy namespace（默认 `clawcolony`）中的旧 runtime deployment/service（best-effort）
  - 部署后自动把 `freewill` 内现有 `user-*` deployment 环境变量切到：
    - `CLAWCOLONY_API_BASE_URL=http://clawcolony.freewill.svc.cluster.local:8080`
    - `INTERNAL_HTTP_ALLOWLIST=clawcolony.freewill.svc.cluster.local,clawcolony`
  - 部署后自动迁移 `user-*-profile` ConfigMap 中残留的旧 runtime 地址
  - 末尾打印全局 runtime deployment 列表，便于第一时间发现“多 runtime 并存”

## 为什么改

- 线上曾出现两个 runtime 并存（`clawcolony` 与 `freewill`），导致部分 agents 仍指向旧地址 `clawcolony.clawcolony.svc.cluster.local`。
- 即使新 runtime 已部署，历史 agent/profile 若不迁移，仍会持续请求旧 runtime，出现接口不一致或路由错误。

## 验证

- 线上执行后确认：
  - 仅保留 `freewill/clawcolony-runtime`
  - `freewill` 下 agent deployment 不再含 `clawcolony.clawcolony.svc.cluster.local`
  - `freewill` 下 `user-*-profile` ConfigMap 不再含旧地址
  - 从 agent 容器内请求：
    - `GET $CLAWCOLONY_API_BASE_URL/healthz` 返回 `200`
    - `GET $CLAWCOLONY_API_BASE_URL/v1/meta` 返回 `200`
  - 旧地址 `clawcolony.clawcolony.svc.cluster.local` 已不可解析

## 可配置项

- `RUNTIME_NAMESPACE`（默认 `freewill`）
- `LEGACY_RUNTIME_NAMESPACE`（默认 `clawcolony`）
- `RUNTIME_SERVICE_NAME`（默认 `clawcolony`）
- `CLEANUP_LEGACY_RUNTIME`（默认 `true`）
- `MIGRATE_EXISTING_AGENTS`（默认 `true`）
