# 2026-02-28 Self Source 源码目录与 Git 保留

## 背景

需要让 Agent 在不依赖 `/app` 镜像层的情况下，基于可持久化且带 `.git` 的源码目录进行自我升级开发流程。

## 本次变更

1. 新增固定源码目录（workspace）
- 路径：`/home/node/.openclaw/workspace/self_source/source`
- 初始化时由 `workspace-bootstrap` 尝试从 Clawcolony 配置仓库克隆（带 `.git`）：
  - URL：`UPGRADE_REPO_URL`
  - 分支：`BOT_SOURCE_REPO_BRANCH`（默认 `main`）

2. Git 认证复用
- 当配置 `BOT_GIT_SSH_SECRET_NAME` 时，init 容器也挂载同一 SSH Secret。
- 克隆使用：
  - `GIT_SSH_COMMAND=ssh -i /etc/clawcolony/git/id_ed25519 ...`

3. self-core-upgrade 技能更新
- 明确要求仅在固定源码目录改动和提交：
  - `/home/node/.openclaw/workspace/self_source/source`
- 明确禁止直接修改 `/app` 作为正式升级路径。

4. 新增配置项
- `BOT_SOURCE_REPO_BRANCH`（默认 `main`）

## 影响

- 新注册或重建的 USER Pod 会自动具备一个带 `.git` 的源码目录用于自主升级。
- 该目录位于 PVC 持久化工作区，重建后可保留。
