# 2026-03-05 · Step 76 · Split 联调一键验收脚本

## 背景
split 架构上线后，手工验证项较多（角色、代理、RBAC、注册烟测），重复排障成本高。

## 目标
提供一个单命令脚本，自动完成 runtime/deployer 分离环境的关键验收，快速判断“可用/不可用”。

## 新增
- 脚本：`scripts/check_split_runtime_deployer.sh`

## 覆盖检查项
1. runtime/deployer `healthz`。
2. `/v1/meta` 角色字段：
   - runtime: `service_role=runtime`、`runtime_enabled=true`、`deployer_enabled=false`
   - deployer: `service_role=deployer`、`runtime_enabled=false`、`deployer_enabled=true`
3. runtime 管理代理链路：
   - `GET /v1/dashboard-admin/openclaw/admin/github/health` -> `200`
4. 角色门禁：
   - runtime 直连 deployer-only 路径 -> `404`
   - deployer 直连 runtime 路径 -> `404`
5. RBAC `can-i`：
   - runtime: 可读 pods，不可创建 deployments，可 `pods/exec`，不可读 nodes
   - deployer: 可创建 deployments，可读 nodes
6. 可选注册烟测（`--register-smoke`）：
   - 通过 runtime 的 dashboard-admin 代理触发 register 并轮询完成。

## 使用
```bash
./scripts/check_split_runtime_deployer.sh --register-smoke
```

常用参数：
- `--use-minikube <true|false>`
- `--ns <namespace>`
- `--runtime-port <port>`
- `--deployer-port <port>`
- `--token <api_token>`

## 本地实测结果
- 命令：`./scripts/check_split_runtime_deployer.sh --register-smoke`
- 结果：`pass=19 fail=0`
- register 烟测：`register_task_id=15`，状态 `succeeded`。

## 结果
split 场景从“手工验收”升级为“可重复自动验收”，后续每次部署后可一键快速定位角色/代理/RBAC 回归问题。
