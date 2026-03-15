# 2026-03-08 Runtime Apply Bootstrap Rollout Fix (Step 84)

## 背景

线上验证发现 `POST /api/v1/prompts/templates/apply` 在 Step 83 路径下全部失败：

- `sync runtime profile files ... mkdir: cannot create directory '/state': Permission denied`

排查后确认：

- 运行态 `bot` 容器内不存在 `/seed` 与 `/state` 路径。
- `/seed` 和 `/state` 仅在 init/bootstrap 路径可用，不能作为 runtime pod exec 的稳定同步源。

## 改动

### 1) 恢复 apply 的 kube bootstrap 生效链路

文件：`internal/server/server.go`

- `syncRuntimeProfileToKube`：
  - `upsertRuntimeProfileConfigMap` 改回返回 `(changed bool, err error)`。
  - 恢复 `patchWorkspaceBootstrapForProfileSync` 调用，按需 rollout user deployment。
- 恢复函数：
  - `patchWorkspaceBootstrapForProfileSync`
  - `patchWorkspaceBootstrapScriptForMCP`
- 移除失效路径：
  - runtime 侧直接在 `bot` 容器执行 `/seed -> /state` 同步脚本。

### 2) RBAC 对齐恢复 deployment update

文件：`k8s/rbac.yaml`

- `configmaps`：保留 `get/list/watch/create/update/patch`
- `deployments`：恢复 `update`（`get/list/watch/update`）

## 测试

- `go test ./...`
- 新/恢复测试：
  - `TestPatchWorkspaceBootstrapScriptForMCP`

## 结果

- apply 生效路径重新与线上容器实际挂载一致，避免 `/seed`/`/state` 路径假设导致的全量失败。
- 通过 rollout 机制确保现有 agents 能拿到更新后的 skills/mcp/openclaw seeds。
