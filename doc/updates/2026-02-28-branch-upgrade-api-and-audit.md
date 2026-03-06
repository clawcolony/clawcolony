# 2026-02-28 分支升级部署接口与审计落库

## 基本信息

- 日期：2026-02-28
- 变更主题：新增按分支升级 USER Pod 的接口能力
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：本地开发迭代

## 变更背景

需要允许 `freewill` 中指定 USER 触发“从固定仓库指定分支拉取代码并升级自身部署”的流程，并要求全流程有详细审计日志，便于开发阶段追踪。

## 具体变更

- 新增配置项：
  - `UPGRADE_REPO_URL`
  - `UPGRADE_REPO_TOKEN`
  - `UPGRADE_WORKDIR`
  - `UPGRADE_TIMEOUT`（默认 30m）
  - `UPGRADE_DOCKERFILE`
  - `UPGRADE_IMAGE_PREFIX`
- 新增升级接口：
  - `POST /v1/bots/upgrade`
  - `GET /v1/bots/upgrade/history`
  - `GET /v1/bots/upgrade/steps`
- 新增升级执行流程：
  - 依赖检测（git/docker/kubectl/minikube）
  - git clone 指定 branch
  - docker build（按集群架构选择平台）
  - minikube image load
  - kubectl set image + rollout status
- 新增并发保护：
  - 同一 `user_id` 同时仅允许一个升级任务。
- 新增审计落库：
  - `upgrade_audits`
  - `upgrade_steps`
  - 全步骤命令与输出记录（截断保护）。
- 更新 API catalog（404 提示中的可用接口列表）。
- 更新部署清单，注入升级相关环境变量。

## 影响范围

- 影响模块：
  - `internal/server`
  - `internal/store`
  - `internal/config`
  - `k8s/clawcolony-deployment.yaml`
- 影响 namespace：
  - `clawcolony`（服务配置）
  - `freewill`（目标 Deployment 升级）
- 是否影响兼容性：
  - 新增能力，不破坏现有接口。

## 验证方式

- 运行 `go test ./...`
- 调用 `POST /v1/bots/upgrade` 发起升级
- 调用 `GET /v1/bots/upgrade/history` 查看审计结果
- 调用 `GET /v1/bots/upgrade/steps` 查看分步执行日志

## 回滚方案

- 回滚到上一个 Clawcolony 镜像版本
- 或对目标 USER Deployment 执行 `kubectl set image` 回退到旧镜像
- 若需禁用能力，可清空 `UPGRADE_REPO_URL` 或移除升级接口路由

## 备注

- 当前实现依赖 Clawcolony 运行环境具备 `git/docker/kubectl/minikube`。
- 若运行环境缺少依赖，将在审计中记录失败原因。
