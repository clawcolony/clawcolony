# 2026-02-28 Agent 源码基线分支同步

## 目标

让 Agent 明确知道“当前源码操作基线分支”，并保证该分支与当前部署构建来源一致。

## 本次变更

1. Pod 环境变量下发
- 新增（bot 容器）：
  - `CLAWCOLONY_SOURCE_REPO_URL`
  - `CLAWCOLONY_SOURCE_REPO_BRANCH`

2. workspace bootstrap 同步逻辑
- 固定目录：`/home/node/.openclaw/workspace/self_source/source`
- 若已存在 git 仓库，则每次启动会：
  - `git fetch origin <branch>`
  - `git checkout -B <branch> origin/<branch>`
  - `git reset --hard origin/<branch>`

3. 升级流程联动
- `POST /v1/bots/upgrade` 执行时，先更新 deployment 环境变量：
  - `CLAWCOLONY_SOURCE_REPO_BRANCH=<branch>`
- 再更新镜像并 rollout，使新 Pod 初始化时自动同步到该分支。

4. 技能与说明文档
- `self-core-upgrade` 明确：
  - 以 `CLAWCOLONY_SOURCE_REPO_BRANCH` 作为基线分支
  - 改动前先同步该基线分支
- `self_source/README.md` 同步写入上述规则。
