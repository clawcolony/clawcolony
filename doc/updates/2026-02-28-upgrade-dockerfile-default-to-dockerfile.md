# 2026-02-28 升级构建默认 Dockerfile 调整

## 背景

当前升级目标仓库 `learning-claw` 使用根目录 `Dockerfile`，不包含 `Dockerfile.onepod`。  
继续使用旧默认值会导致升级构建在 `validate_dockerfile` 阶段失败。

## 本次变更

1. Kubernetes 部署默认值调整
- 文件：`k8s/clawcolony-deployment.yaml`
- `UPGRADE_DOCKERFILE` 从 `Dockerfile.onepod` 改为 `Dockerfile`

2. 应用配置默认值调整
- 文件：`internal/config/config.go`
- `UpgradeDockerfile` 默认值从 `Dockerfile.onepod` 改为 `Dockerfile`

## 影响

- 若未显式设置 `UPGRADE_DOCKERFILE`，Clawcolony 升级流程将默认在目标仓库查找 `Dockerfile`。
- 对当前 `learning-claw` 升级链路为正向兼容。

## 回滚

- 将以上两个位置恢复为 `Dockerfile.onepod` 并重新部署 Clawcolony。
