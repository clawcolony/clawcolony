# 2026-03-05 · Step 75 · Runtime/Deployer RBAC 拆分与最小权限

## 背景
前两步完成了服务角色与流量拆分，但 runtime/deployer 仍共用同一个 ServiceAccount，权限边界不够清晰。

## 目标
1. runtime 不再持有部署写权限；
2. deployer 保留部署管理权限；
3. 保证 runtime 仍可执行 OpenClaw 聊天所需的 `pods/exec` 与日志读取。

## 实现内容

### 1) ServiceAccount 拆分
- `clawcolony-runtime-sa`
- `clawcolony-deployer-sa`

### 2) Role / RoleBinding 拆分
- `clawcolony` namespace：
  - `clawcolony-runtime-self-role`：只读 `pods/services/configmaps/secrets`
  - `clawcolony-deployer-self-role`：读写 `pods/services/configmaps/secrets` + `apps/deployments/replicasets`
- `freewill` namespace：
  - `clawcolony-runtime-user-role`：
    - 读 `pods`, `pods/log`, `apps/deployments`
    - `create pods/exec`（用于 chat 调用 `openclaw agent`）
  - `clawcolony-deployer-user-role`：
    - 读写 `pods/pods/log/services/configmaps/secrets/persistentvolumeclaims`
    - 读写 `apps/deployments/replicasets`
    - `create pods/exec`

### 3) ClusterRole 仅给 deployer
- `clawcolony-deployer-node-reader`
- 仅绑定到 `clawcolony-deployer-sa`

### 4) Deployment 对应 SA
- `k8s/clawcolony-runtime-deployment.yaml` -> `serviceAccountName: clawcolony-runtime-sa`
- `k8s/clawcolony-deployer-svc-deployment.yaml` -> `serviceAccountName: clawcolony-deployer-sa`
- `k8s/clawcolony-deployment.yaml`（all 模式兼容）-> `serviceAccountName: clawcolony-deployer-sa`

## 本地验证（Minikube, split）

### RBAC 授权检查
- runtime SA：
  - `get pods -n freewill` -> `yes`
  - `create deployments.apps -n freewill` -> `no`
  - `create pods --subresource=exec -n freewill` -> `yes`
  - `get nodes` -> `no`
- deployer SA：
  - `create deployments.apps -n freewill` -> `yes`
  - `create secrets -n freewill` -> `yes`
  - `get nodes` -> `yes`

### 功能回归
- runtime `/v1/meta` 正常，角色为 `runtime`；
- runtime `dashboard-admin` 代理调用 `register` 成功：
  - `register_task_id=13`
  - `status=succeeded`
  - 新 user `user-1772767294735-1902` 已运行；
- runtime `/v1/chat/send` 仍可正常排队执行并收到 bot 回复（验证 `pods/exec` 权限链路未被破坏）。

## 结果
runtime 与 deployer 的 K8s 权限边界在清单层面完成拆分，且 split 模式核心链路（管理代理 + 注册 + 聊天执行）保持可用。
