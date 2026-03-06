# 2026-02-26 AI Bot 平台自适应构建脚本

## 基本信息

- 日期：2026-02-26
- 变更主题：按目标平台自动构建并加载 AI Bot 镜像
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

AI Bot（如 OpenClaw）需要保证构建产物与目标 Kubernetes 运行平台一致（linux/amd64 或 linux/arm64），避免架构不匹配导致 Pod 启动失败。

## 具体变更

- 新增脚本 `scripts/build_bot_image_for_minikube.sh`
- 脚本逻辑：
  - 优先读取 Minikube 节点架构（`kubectl get nodes`）
  - 映射为 Docker 平台（`linux/amd64` / `linux/arm64`）
  - 执行 `docker build --platform ...`
  - 执行 `minikube image load ...`
- 更新 README，新增 AI Bot 平台自适应构建用法说明

## 影响范围

- 影响模块：脚本、文档
- 影响 namespace：无
- 是否影响兼容性：否（新增能力）

## 验证方式

- `./scripts/build_bot_image_for_minikube.sh --context <path> --dockerfile <file> --image <tag>`
- 验证输出包含目标平台与镜像加载结果

## 回滚方案

- 删除该脚本并从 README 移除对应说明

## 备注

当无法读取集群架构时，脚本会回退到 `uname -m` 主机架构进行平台判断。
