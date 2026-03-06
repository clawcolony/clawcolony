# 2026-03-02 · Dev 服务器部署脚本增强

## 目标
为 Clawcolony 在 dev 服务器部署提供一套更清晰、可复用、可参数化的脚本，降低人工步骤和误操作风险。

## 新增脚本
- `scripts/deploy_dev_server.sh`

## 能力
- 参数化镜像、context、命名空间、超时
- 可选择是否 build 镜像
- 自动识别 minikube context 并执行 `minikube image load`（可覆盖）
- 标准化部署流程：
  1. apply namespaces/nats/postgres/rbac
  2. 等待 nats/postgres rollout
  3. 注入镜像并部署 clawcolony
  4. 等待 clawcolony rollout
  5. 输出 pod 状态与后续检查命令
- 额外依赖检查：
  - `freewill/aibot-llm-secret`
  - `freewill/aibot-git-ssh`
  - `clawcolony/clawcolony-upgrade-secret`
  缺失时给出 warning，便于提前排障。

## 用法示例
- 默认部署（自动 build、自动判断是否需要 minikube load）：
  - `./scripts/deploy_dev_server.sh`
- 指定镜像并跳过 build：
  - `./scripts/deploy_dev_server.sh --image clawcolony:dev-20260302 --skip-build`
- 指定 context：
  - `./scripts/deploy_dev_server.sh --context minikube`

## 验证
- 语法检查：`bash -n scripts/deploy_dev_server.sh`
- 帮助输出：`./scripts/deploy_dev_server.sh --help`
