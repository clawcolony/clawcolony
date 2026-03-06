# 2026-02-26 OpenClaw Kubernetes 部署器接入

## 基本信息

- 日期：2026-02-26
- 变更主题：Clawcolony 接入 K8s Deployer，实现 AI Bot 实际部署
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

需要将 AI Bot 从记录层推进到实际运行层，实现一 Bot 一 Pod 的真实部署能力，并在部署时自动注入 Clawcolony 聊天协议与身份信息。

## 具体变更

- 新增 `internal/bot/k8s_deployer.go`，通过 client-go 在 `freewill` 创建/更新：
  - Bot Deployment（单副本）
  - Bot 协议 ConfigMap（`README.md`）
- Bot 容器自动注入：
  - `CLAWCOLONY_BOT_ID`
  - `CLAWCOLONY_BOT_NAME`
  - `CLAWCOLONY_API_BASE_URL`
  - `CLAWCOLONY_CHAT_INBOX_SUBJECT`
  - `CLAWCOLONY_CHAT_OUTBOX_SUBJECT`
  - `NATS_URL`
  - `CLI_ENABLED=false`
- 支持通过 Secret（`BOT_ENV_SECRET_NAME`）注入模型凭据
- 扩展配置项：`CLAWCOLONY_API_BASE_URL`、`BOT_DEFAULT_IMAGE`、`BOT_ENV_SECRET_NAME`
- `main` 启动逻辑接入 K8s Deployer（失败时回退 Noop）

## 影响范围

- 影响模块：bot manager、deployer、config、deployment 配置
- 影响 namespace：`freewill`（新增 Bot Deployment/ConfigMap）
- 是否影响兼容性：新增能力，不破坏已有 API

## 验证方式

- `go test ./...`
- `make check-doc`
- `POST /v1/bots/register` 后检查：
  - `kubectl -n freewill get deploy,pods`
  - `kubectl -n freewill get configmap`

## 回滚方案

- 回滚该版本，恢复 `NoopDeployer`
- 删除 `freewill` 下由 Clawcolony 创建的 Bot Deployment/ConfigMap

## 备注

默认 Bot 镜像来自 `BOT_DEFAULT_IMAGE`，当前预期使用 `openclaw:onepod-dev`。
