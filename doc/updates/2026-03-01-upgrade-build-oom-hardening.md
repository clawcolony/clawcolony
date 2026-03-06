# 2026-03-01 Upgrade Build OOM Hardening

## 背景
在 `POST /v1/bots/upgrade` 触发的自升级流程中，`docker build` 出现 `signal: killed`，导致镜像未构建完成、后续 rollout 无法进行。

## 变更
- 升级构建命令改为启用 BuildKit + plain 日志：
  - `DOCKER_BUILDKIT=1`
  - `--progress=plain`
- 新增构建资源与行为配置（环境变量）：
  - `UPGRADE_DOCKER_BUILD_MEMORY`
  - `UPGRADE_DOCKER_BUILD_CPUS`
  - `UPGRADE_DOCKER_BUILD_NO_CACHE`
  - `UPGRADE_DOCKER_BUILD_ARGS`
- 当构建错误命中 `signal: killed / oom / out of memory` 关键词时，返回明确的 OOM 诊断提示，指导调参。

## 目的
- 降低在开发环境（本地 Docker / Minikube）中因资源不足导致升级失败的概率。
- 提高失败可观测性，避免只看到“构建失败”而无法快速定位资源问题。

## 建议默认值（开发环境）
- `UPGRADE_DOCKER_BUILD_MEMORY=2g`
- `UPGRADE_DOCKER_BUILD_CPUS=2`
- `UPGRADE_DOCKER_BUILD_NO_CACHE=true`（仅当缓存异常或高内存峰值时启用）
