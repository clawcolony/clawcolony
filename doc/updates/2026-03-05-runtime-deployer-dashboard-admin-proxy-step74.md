# 2026-03-05 · Step 74 · Runtime Dashboard Admin 代理到 Deployer

## 背景
Step 72/73 完成了 runtime 与 deployer 的服务拆分，但 Dashboard 里的 OpenClaw 管理与 Prompt 应用仍直接打 deployer-only 接口：

- `/v1/openclaw/admin/*`
- `/v1/bots/upgrade*`
- `/v1/prompts/templates/apply`

在 `runtime` 角色下这些接口被门禁拦截，导致页面不可用。

## 目标
在保持角色隔离前提下，让用户仍可通过 runtime 的 Dashboard 使用管理能力：

1. 前端只访问 runtime；
2. runtime 仅对允许清单做代理转发；
3. 实际执行仍在 deployer；
4. `all` 单体模式保持兼容。

## 实现内容

### 1) 新增 runtime 侧 dashboard-admin 代理路由
- 文件：`internal/server/server.go`
- 新路由前缀：`/v1/dashboard-admin/*`
- 允许目标白名单：
  - `/v1/prompts/templates/apply`
  - `/v1/bots/upgrade`
  - `/v1/bots/upgrade/task`
  - `/v1/bots/upgrade/history`
  - `/v1/bots/upgrade/steps`
  - `/v1/openclaw/admin/overview`
  - `/v1/openclaw/admin/action`
  - `/v1/openclaw/admin/register/task`
  - `/v1/openclaw/admin/register/history`
  - `/v1/openclaw/admin/github/health`

### 2) 按角色处理代理行为
- `runtime`（且非 `deployer`）：
  - 转发到 `CLAWCOLONY_DEPLOYER_API_BASE_URL`。
- `all` 或 `deployer`：
  - 本地 dispatch 到原 handler，兼容单体部署与开发态。

### 3) 配置项补充
- 文件：`internal/config/config.go`
- 新增：`DeployerAPIBase`
- 环境变量：`CLAWCOLONY_DEPLOYER_API_BASE_URL`
- 默认值：
  - `http://clawcolony-deployer.clawcolony.svc.cluster.local:8080`

并在 `/v1/meta` 中增加 `deployer_api_base` 可观测字段。

### 4) Dashboard 前端改造
- 文件：
  - `internal/server/web/dashboard_openclaw_pods.html`
  - `internal/server/web/dashboard_prompts.html`
- 将原 deployer-only 直连路径改为 `/v1/dashboard-admin/*`。

### 5) 部署与脚本同步
- 文件：
  - `k8s/clawcolony-runtime-deployment.yaml`
  - `k8s/clawcolony-deployer-svc-deployment.yaml`
  - `k8s/clawcolony-deployment.yaml`
  - `scripts/bootstrap_full_stack.sh`
  - `scripts/oneclick.env.example`
- 注入并透传 `CLAWCOLONY_DEPLOYER_API_BASE_URL`。

## 测试

### 单元测试
- `go test ./internal/config ./internal/server -run "TestDashboardAdminProxy|TestRoleAccess"` ✅
- 新增覆盖：
  - runtime 模式代理到上游 deployer；
  - all 模式本地 dispatch。

### 本地 Minikube 联调（split）
1. runtime `/v1/meta` 返回：
   - `service_role=runtime`
   - `deployer_api_base=http://clawcolony-deployer.clawcolony.svc.cluster.local:8080`
2. runtime 调用：
   - `GET /v1/dashboard-admin/openclaw/admin/overview` -> `HTTP 200`
   - `GET /v1/dashboard-admin/openclaw/admin/github/health` -> `HTTP 200`
3. deployer 直连：
   - `GET /v1/openclaw/admin/overview` -> `HTTP 200`
   - `GET /v1/mail/contacts` -> `HTTP 404`（角色门禁生效）
4. runtime 经代理触发注册（GitHub mock）：
   - `POST /v1/dashboard-admin/openclaw/admin/action {"action":"register"}` -> 返回 `register_task_id=12`
   - 轮询 `GET /v1/dashboard-admin/openclaw/admin/register/task?register_task_id=12` -> `status=succeeded`
   - 新 user `user-1772766916067-7740` deployment/pod 均为 `Running`

## 结果
在 split 架构下，Dashboard 管理页可以继续从 runtime 入口使用 deployer 能力，同时不破坏 deployer-only 接口隔离边界。
