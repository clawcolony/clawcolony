# 2026-03-03 新建 OpenClaw Pod：按最新 Release 构建 + 代码仓库对齐

## 变更目标
在 `register` 新建 user pod 时：

1. 优先查询 `openclaw/openclaw` 最新 release tag。
2. 若存在 release：
   - 用该 tag 源码尝试构建镜像并加载到 minikube。
   - 新建 user 专属 repo 时，用该 tag 的代码快照初始化。
3. 若不存在 release（或 release 查询失败）：
   - 使用现有 `BOT_DEFAULT_IMAGE` 启动。
4. 每个 user 使用独立 git secret（私钥 + known_hosts），用于 pod 内 git 操作。

## 具体变更
1. `POST /v1/openclaw/admin/action` 的 `action=register` 改为统一 provisioning 链路：
   - 读取 `clawcolony-github`（`GITHUB_TOKEN/GITHUB_OWNER/GITHUB_MACHINE_USER`）
   - 分配人类可读且不重复的用户名（`name_pool`，冲突后 `adj-name`）
   - 创建 repo：`<owner>/openclaw-<username>`
   - 从上游 `openclaw/openclaw` 的 `source_ref`（优先 latest release tag）浅克隆并 push 到目标 repo 的 `main`
   - 生成 deploy key（ed25519）并注册到目标 repo
   - 在 user namespace 写入 per-user secret：`aibot-git-<user_id>`
   - 若有 latest release tag：尝试构建镜像 `openclaw:release-<tag>-<ts>` 并 `minikube image load`
   - 调用 `RegisterAndInit` 部署 user pod

2. 新增返回字段（register 响应）：
   - `repo_full_name`
   - `repo_url_ssh`
   - `git_secret_name`
   - `source_ref`
   - `release_tag`
   - `image`
   - `image_built`
   - `image_build_note`

## 行为说明
- 当 release 构建失败时：回退 `BOT_DEFAULT_IMAGE`，不中断 register。
- 用户源码仓库始终初始化为“本次选用的 source_ref 快照”。
- 部署给 pod 的 `SourceRepoBranch` 固定写 `main`（因为快照已推到目标 repo 的 main）。

## 影响范围
- `internal/server/openclaw_admin.go`
- register 管理流程与返回 payload

## 验证方式
1. 调用 register：
   - `POST /v1/openclaw/admin/action` with `{"action":"register"}`
2. 检查响应是否包含 `release_tag/image/image_built/repo_full_name`。
3. 检查 GitHub：是否出现 `openclaw-<username>`，且有 deploy key。
4. 检查 k8s：`freewill` 下是否有 `aibot-git-<user_id>` secret。
5. 检查 pod：`self_source/source/.git` 是否存在，`origin` 指向新 repo。

## 回滚说明
- 回滚到旧 register 逻辑：
  - 将 `handleOpenClawAdminRegister` 恢复为直接 `RegisterAndInit(Provider, Image)`。
  - 移除/停用 provisioning helper。
