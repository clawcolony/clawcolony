# 2026-03-03 OpenClaw 新建账户流程 - 第 3 步实现设计（Git 凭据下发）

## 目标
当 Clawcolony 创建一个新的 OpenClaw user pod 时，自动完成以下动作并保证可审计、可重试、可回滚：

1. 生成唯一且可读的用户名（如 `roy`；冲突时 `happy-roy`）。
2. 在 GitHub 组织 `clawcolony` 下创建仓库 `openclaw-<username>`（来源 `openclaw/openclaw`）。
3. 为该仓库创建独立 deploy key（每个 user 独立密钥）。
4. 将私钥保存为该 user 专属 k8s Secret，并用于 pod 内 git 操作。

## 非目标（本阶段不做）
- 不创建 GitHub machine user（继续复用现有 `claw-archivist` PAT 操作 GitHub API）。
- 不做多平台分发策略。
- 不做仓库模板复杂参数化（仅固定仓库源 + 默认分支）。

## 输入与依赖
- 已存在 k8s Secret：`clawcolony-github`（namespace: `clawcolony`）
  - `GITHUB_TOKEN`
  - `GITHUB_OWNER=clawcolony`
  - `GITHUB_MACHINE_USER=claw-archivist`
- 固定 upstream：`https://github.com/openclaw/openclaw`
- 目标 user namespace：`freewill`

## 核心数据与命名
- 用户名：全局唯一，不允许跨时间复用。
- repo 名：`openclaw-<username>`
- user_id：沿用现有 `user-<timestamp>-<rand>`
- user git secret：`aibot-git-<user_id>`（namespace: `freewill`）
- deploy key title：`clawcolony-<user_id>-<unix_ts>`

## 用户名分配策略

### 名字池
- `name_pool`：常见中性人名（英文）。
- `adj_pool`：中性、无歧义前缀（如 `calm`, `brisk`, `clear`）。

### 分配算法
1. 优先从 `name_pool` 取一个候选 `name`。
2. 若冲突（以下任一即冲突）：
   - DB 已存在同名 `user_name`；
   - 组织里已存在同名 repo `openclaw-<name>`；
3. 则尝试 `adj-name`；若仍冲突，轮换新 `adj`。
4. 超过最大尝试次数返回 `username_exhausted`。

> 冲突检查必须“DB + GitHub 双检查”。

## GitHub Provision 流程

### 1) 创建仓库
- 优先方案：`POST /orgs/{owner}/repos`
  - repo: `openclaw-<username>`
  - `private=true`
  - `auto_init=false`
- 初始化代码：
  - `git clone --mirror https://github.com/openclaw/openclaw`
  - push 到新 repo 默认分支（`main`）

### 2) 生成 deploy key
- 由 Clawcolony 服务端生成 ED25519 keypair。
- GitHub API：`POST /repos/{owner}/{repo}/keys`
  - `title`, `key`, `read_only=false`（允许 agent push）

### 3) 写入 k8s secret
- 在 `freewill` namespace 创建：`aibot-git-<user_id>`
- keys：
  - `id_ed25519`（私钥）
  - `known_hosts`（至少含 `github.com`）

### 4) 调用现有注册部署
- `RegisterAndInit(DeploySpec)` 传入：
  - `BotID`, `Name`
  - `SourceRepoURL=git@github.com:clawcolony/openclaw-<username>.git`
  - `SourceRepoBranch=main`
  - `GitSSHSecretName=aibot-git-<user_id>`

## 状态机与幂等

### 状态
- `allocated` -> `repo_created` -> `key_created` -> `secret_created` -> `deployed`
- 失败态：`failed_<step>`

### 幂等策略
- 同一个 `request_id` 重试时：
  - 已创建 repo：跳过创建。
  - 已存在 deploy key（同 title 或同 pubkey）：跳过。
  - 已存在 k8s secret：update 覆盖。
  - 最终部署失败可重复触发（不重复创建用户名）。

## 失败补偿（回滚）
- repo 创建成功但后续失败：默认保留 repo（便于排障）。
- deploy key 创建成功但 secret 写入失败：删除该 key（可选，建议做）。
- secret 创建成功但部署失败：保留 secret，不自动删。

## 观测与审计
- 新增审计事件（结构化日志 + DB）：
  - `github_repo_create`
  - `github_deploy_key_create`
  - `k8s_git_secret_upsert`
  - `bot_register_deploy`
- 每步记录：
  - `request_id`, `user_id`, `username`, `repo`, `status`, `error`, `duration_ms`

## API 设计（管理端）

### 1) 注册并自动完成 GitHub Provision
- `POST /v1/openclaw/admin/action`
- body:
```json
{
  "action": "register"
}
```
- 返回（accepted）：
```json
{
  "action": "register",
  "item": {
    "user_id": "user-...",
    "user_name": "roy"
  },
  "provision": {
    "repo": "clawcolony/openclaw-roy",
    "git_secret_name": "aibot-git-user-...",
    "branch": "main"
  }
}
```

### 2) 可选：预检接口（不创建，仅校验）
- `GET /v1/openclaw/admin/github/health`（已存在）
- 可扩展增加：`name_pool_available`, `token_scope_ok`

## 安全约束
- PAT 仅保存在 `clawcolony-github`，不落盘。
- API 响应不返回私钥。
- deploy key 私钥仅进入 user 专属 Secret。
- 日志中对 token/私钥做全量脱敏。

## 与现有架构的冲突评估
- 与现有 `RegisterAndInit` 兼容：已支持传入 `BotID/Name/SourceRepoURL/Branch/GitSSHSecretName`。
- 与现有 k8s deployer 兼容：已支持按 spec 覆盖 repo/branch/git secret。
- 主要新增点在 `openclaw_admin register` 的前置 provisioning 编排。

## 需要你提供/确认的项
1. PAT scopes 最小集合（建议）：
   - `repo`
   - `admin:public_key`（若仅 deploy key 可能需要 repo admin 权限；实际以 GitHub 返回为准）
2. `name_pool` 与 `adj_pool` 初始词库（我可先内置一版，后续迁移 DB）。
3. 新 repo 默认可见性：`private`（默认）是否确认。

## 实施拆分（建议）
- Phase A（先跑通一个账户）：
  1. register 流程中串行 provisioning
  2. 创建 repo + deploy key + k8s secret
  3. 调用 RegisterAndInit
- Phase B（稳定性）：
  1. request_id 幂等
  2. 失败补偿
  3. 审计查询接口
- Phase C（可运营）：
  1. name/adj 池配置化（DB）
  2. dashboard 显示 provisioning 细节

## 验证用例（单账户）
1. 调 `POST /v1/openclaw/admin/action {"action":"register"}`。
2. 期望：
   - DB 新增 user；
   - GitHub 出现 `openclaw-<username>`；
   - 仓库 Deploy Keys 出现新 key；
   - `freewill` 出现 `aibot-git-<user_id>`；
   - pod 启动后 `self_source/source/.git` 正常、`origin` 指向该 repo。

