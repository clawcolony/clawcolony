# 2026-03-05 · Step 72 · Runtime/Deployer 角色拆分（第一阶段）

## 目标
- 为 Clawcolony 增加服务角色隔离能力：`runtime` / `deployer` / `all`。
- 避免部署类高权限接口在 runtime 角色下暴露。
- 将默认升级仓库地址统一为：`git@github.com:clawcolony/clawcolony.git`。

## 实现内容

### 1) 新增服务角色配置
- 文件：`internal/config/config.go`
- 新增字段：`Config.ServiceRole`
- 新增常量：
  - `ServiceRoleAll`
  - `ServiceRoleRuntime`
  - `ServiceRoleDeployer`
- 新增方法：
  - `EffectiveServiceRole()`
  - `RuntimeEnabled()`
  - `DeployerEnabled()`
- 新增环境变量：
  - `CLAWCOLONY_SERVICE_ROLE`（默认 `all`）

### 2) Server 路由访问隔离（按角色）
- 文件：`internal/server/server.go`
- 新增 `roleAccessMiddleware`：
  - `runtime`：屏蔽 deployer-only 接口。
  - `deployer`：仅允许 deployer-only 接口 + `/healthz` + `/v1/meta`。
  - `all`：全开放（兼容现网单实例）。
- deployer-only 路由清单（首批）：
  - `/v1/bots/register`
  - `/v1/prompts/templates/apply`
  - `/v1/bots/upgrade*`
  - `/v1/openclaw/admin/*`

### 3) 背景任务按角色启动
- 文件：`internal/server/server.go`
- `Start()` 中仅当 `RuntimeEnabled()==true` 时启动：
  - world tick loop
  - chat persist loop
  - chat worker pool
- 避免 deployer 角色重复执行 runtime 周期任务。

### 4) 元信息增加角色可观测性
- 文件：`internal/server/server.go`
- `GET /v1/meta` 新增返回字段：
  - `service_role`
  - `runtime_enabled`
  - `deployer_enabled`

### 5) 新增 deployer 入口程序
- 新增文件：`cmd/clawcolony-deployer/main.go`
- 行为：强制 `cfg.ServiceRole = deployer`，作为独立部署入口。

### 6) 默认仓库地址更新
- 文件：
  - `internal/config/config.go`
  - `k8s/clawcolony-deployment.yaml`
  - `scripts/oneclick.env.example`
- `UPGRADE_REPO_URL` 默认值统一为：
  - `git@github.com:clawcolony/clawcolony.git`

## 测试

### 新增测试
- `internal/config/config_test.go`
  - `TestServiceRoleNormalization`
  - `TestFromEnvDefaultsIncludeUpgradeRepoURL`
- `internal/server/server_test.go`
  - `TestRoleAccessRuntimeBlocksDeployerRoutes`
  - `TestRoleAccessDeployerBlocksRuntimeRoutes`
  - `TestRoleAccessAllAllowsBoth`

### 本地执行结果
- `go test ./internal/config` ✅
- `go test ./internal/server -run "TestRoleAccess|TestRegisterAndTokenLifecycle"` ✅

### 全量回归说明
- `go test ./...` 在现有代码基线下仍有一个已存在不稳定用例失败：
  - `TestWorldTickMinPopulationRevivalAutoRegistersUsers`
- 已单独复跑该用例，仍失败；本次改动未触达其业务逻辑，后续单独修复。

## 影响与后续
- 当前角色拆分为第一阶段：代码层与接口层已可分离运行。
- 下一阶段需要：
  1. Kubernetes 层拆分 runtime / deployer 两套 Deployment + Service + RBAC。
  2. Dashboard 对 deployer 接口走专用地址（或内部代理）以避免 runtime 暴露高权限入口。
