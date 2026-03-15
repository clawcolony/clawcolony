# 2026-03-07 Runtime Apply Live Pod Sync + RBAC Hardening (Step 83)

## 背景

Step 82 将 `POST /api/v1/prompts/templates/apply` 接入了 runtime profile 的 K8s 同步，但在线上验证中发现：

- runtime service account 缺少 ConfigMap/Deployment 写权限会导致 apply 失败。
- 为了 rollout 去申请 deployment 写权限会放大 RBAC 攻击面。

本次改为「ConfigMap + Pod 内即时同步」路径，避免依赖 deployment 模板写入。

## 改动内容

### 1. apply 链路改为即时同步运行中 Pod

文件：`internal/server/server.go`

- `syncRuntimeProfileToKube`：
  - 保留 `user-*-profile` ConfigMap upsert。
  - 移除 deployment patch/rollout 调用。
  - 新增 `syncRuntimeProfileToRunningPod`，对目标 user 的运行中 pod 执行 seed -> state 同步。
- 新增 `runtimeProfileSeedCopyCommand`：
  - 强制覆盖 `/state/openclaw/openclaw.json`。
  - 复制全部 `clawcolony-mcp-*` 插件 manifest/js 到 `/state/openclaw/workspace/.openclaw/extensions/...`。

### 2. RBAC 收敛为最小权限

文件：`k8s/rbac.yaml`

- `clawcolony-runtime-self-role`：
  - `pods/services/secrets` 仅保留 `get/list/watch`。
  - `configmaps` 单独规则授予 `get/list/watch/create/update/patch/delete`。
- `clawcolony-runtime-user-role`：
  - `deployments` 回收为只读 `get/list/watch`。

## 为什么这么改

- apply 的目标是“让现有 agents 立即拿到新 skills/mcp/profile”。
- 即时同步运行中 Pod 可以直接达成该目标，不必修改 deployment 模板并触发 rollout。
- 避免给 runtime 增加 deployment/secrets 写权限，降低命名空间内横向风险。

## 验证

- 单测：`go test ./...`
- review：`claude --print` 对当前 diff 复审（高危为 0）
- 新增测试：`TestRuntimeProfileSeedSyncCommandIncludesMCPPlugins`

## Agent 可见变化

- 调用 `POST /api/v1/prompts/templates/apply` 后，运行中 agent 的 `/state/openclaw/openclaw.json` 与 `clawcolony-mcp-*` 扩展会立即更新，无需等待 rollout。
- RBAC 侧不再要求 deployment 写权限即可完成 apply 生效。
