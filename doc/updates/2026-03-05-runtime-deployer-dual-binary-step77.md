# 2026-03-05 · Step 77 · 镜像双二进制与 Deployer 固定入口

## 背景
虽然代码已新增 `cmd/clawcolony-deployer`，但镜像此前仅构建 `/clawcolony`，deployer 实际仍依赖环境变量切换角色。

## 目标
1. 镜像同时包含 runtime 与 deployer 两个可执行文件；
2. deployer Deployment 固定启动 deployer 二进制，避免入口漂移；
3. split 部署后保持功能可用。

## 变更

### 1) Dockerfile
- 文件：`Dockerfile`
- builder 阶段同时构建：
  - `/out/clawcolony`
  - `/out/clawcolony-deployer`
- 运行时镜像同时拷贝：
  - `/clawcolony`
  - `/clawcolony-deployer`

### 2) Deployer Deployment 固定 command
- 文件：`k8s/clawcolony-deployer-svc-deployment.yaml`
- 新增：
  - `command: ["/clawcolony-deployer"]`

## 验证
1. 编译验证：
   - `go build ./cmd/clawcolony ./cmd/clawcolony-deployer` ✅
2. split 重部署：
   - `./scripts/deploy_dev_server.sh --split-services --image clawcolony:split-step77-...` ✅
3. 生效确认：
   - `kubectl -n clawcolony get deploy clawcolony-deployer -o jsonpath='{...command}'` -> `["/clawcolony-deployer"]`
   - deployer 日志首行：
     - `clawcolony-deployer starting on :8080 (service_role=deployer)`
4. 回归验收：
   - `./scripts/check_split_runtime_deployer.sh` -> `pass=17 fail=0` ✅

## 结果
deployer 从“共享入口 + 角色变量”升级为“专用入口 + 独立二进制”，split 运行边界更稳固。
