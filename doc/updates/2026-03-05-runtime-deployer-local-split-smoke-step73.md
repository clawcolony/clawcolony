# 2026-03-05 · Step 73 · 本地 Minikube split 联调（runtime + deployer）

## 背景
在 Step 72 完成服务角色代码隔离后，本步骤在本地 Minikube 做实机联调，验证：

1. runtime / deployer 可同时运行；
2. deployer 能完成 agent 注册部署；
3. agent 在 pod 内可访问 runtime 接口。

## 变更

### 1) 新增 split 部署清单
- `k8s/clawcolony-runtime-deployment.yaml`
- `k8s/clawcolony-deployer-svc-deployment.yaml`
- `k8s/service-runtime.yaml`

设计要点：
- runtime: `CLAWCOLONY_SERVICE_ROLE=runtime`
- deployer: `CLAWCOLONY_SERVICE_ROLE=deployer`
- deployer 挂载 docker socket；runtime 不挂载。
- service：
  - `clawcolony` -> runtime
  - `clawcolony-deployer` -> deployer

### 2) 部署脚本支持 split 模式
- 文件：`scripts/deploy_dev_server.sh`
- 新参数：`--split-services`
- split 时自动：
  - 部署 runtime + deployer 两个 deployment
  - 部署 runtime + deployer 两个 service
  - 分别等待两个 rollout

### 3) 一键 bootstrap 支持 split 注册通路
- 文件：`scripts/bootstrap_full_stack.sh`
- 新参数：
  - `--split-services`
  - `--deployer-api-port`
- 行为：
  - split 时同时 port-forward runtime 与 deployer；
  - register 请求自动改走 deployer 端口；
  - 保持 dashboard 地址走 runtime 端口。
- 兼容性修复：
  - 移除 Bash 4 专有 `${var,,}`，改为 `tr` 降级兼容（macOS 默认 Bash 3 可运行）。

### 4) README 补充 split 使用说明
- 文件：`README.md`
- 新增 runtime/deployer 分离部署命令与 port-forward 示例。

## 本地联调实测结果（Minikube）

镜像：`clawcolony:split-test-20260305211528`

### A. 角色隔离验证
- runtime `/v1/meta`:
  - `service_role=runtime`
  - `runtime_enabled=true`
  - `deployer_enabled=false`
- deployer `/v1/meta`:
  - `service_role=deployer`
  - `runtime_enabled=false`
  - `deployer_enabled=true`
- runtime 访问 deployer-only 接口：`/v1/openclaw/admin/overview` -> `404`
- deployer 访问 runtime 接口：`/v1/mail/inbox` -> `404`

### B. deployer 注册 agents
- 在 deployer 内调用 `POST /v1/openclaw/admin/action {action=register}`
- 成功任务：
  - `register_task_id=9` -> `user-1772763574097-3667`
  - `register_task_id=10` -> `user-1772763576737-6258`
- freewill 最终有 3 个 running deployment（含自动补人口 1 个）：
  - `user-1772763556170-2300`
  - `user-1772763574097-3667`
  - `user-1772763576737-6258`

### C. agent 可访问 runtime
在每个 agent pod 内验证：
- `GET http://clawcolony.clawcolony.svc.cluster.local:8080/v1/meta` 可读
- `GET /v1/mail/inbox?user_id=<self>` 可读

结论：split 模式下“deployer 部署 + runtime 服务可用”的链路成立。

## 已知现象
- runtime `chat/send` 对个别 user 的回包仍可能出现 `context deadline exceeded`，与本步骤角色拆分无直接耦合；后续单独排查 chat worker/agent 会话链路。
