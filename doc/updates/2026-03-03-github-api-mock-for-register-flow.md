# 2026-03-03 GitHub API Mock（用于 register 开发测试）

## 背景
`register` 流程会频繁调用 GitHub：查询 release、创建 repo、创建 deploy key。开发联调阶段反复触发，容易撞到 GitHub 速率限制或 abuse 检测。

## 本次变更
新增 GitHub Mock 模式（默认关闭），用于本地/开发环境快速验证流程，不触发真实 GitHub API。

### 新增配置项
- `GITHUB_API_MOCK_ENABLED`（默认 `false`）
- `GITHUB_API_MOCK_OWNER`（默认 `clawcolony`）
- `GITHUB_API_MOCK_MACHINE_USER`（默认 `claw-archivist`）
- `GITHUB_API_MOCK_RELEASE_TAG`（默认空；mock 下若为空会回退到内置 `v2026.3.1`）

### Mock 行为
当 `GITHUB_API_MOCK_ENABLED=true` 时：
1. `/v1/openclaw/admin/github/health`
   - 返回 `mock_mode=true`
   - 不依赖 k8s secret
2. register 链路中的 GitHub 行为改为内存模拟：
   - repo 名称占用检查（基于进程内 map）
   - 创建 repo（写入内存）
   - 创建 deploy key（写入内存）
   - 查询 latest release（返回 mock release tag）
3. 跳过 release 镜像构建（避免每次测试都 docker build）
   - 回退使用 `BOT_DEFAULT_IMAGE`
4. deploy 用于源码拉取的地址：
   - 使用现有 `UPGRADE_REPO_URL`（若配置）
   - git secret 使用 `BOT_GIT_SSH_SECRET_NAME`

## 影响范围
- `internal/config/config.go`
- `internal/server/server.go`
- `internal/server/openclaw_admin.go`
- `k8s/clawcolony-deployment.yaml`（补充 mock env）

## 风险与边界
- Mock 仅用于开发验证流程；不会真实创建 GitHub repo/key。
- 进程重启后，内存态 mock repo/key 记录会丢失。
- 生产环境应保持 `GITHUB_API_MOCK_ENABLED=false`。

## 验证
1. 打开 mock：
   - `kubectl -n <ns> set env deployment/<app> GITHUB_API_MOCK_ENABLED=true`
2. 检查 health：
   - `GET /v1/openclaw/admin/github/health` 返回 `checks.mock_mode=true`
3. 触发 register：
   - `POST /v1/openclaw/admin/action {"action":"register"}`
   - 响应应成功，且 `image_built=false`、`image_build_note` 提示 mock 模式。
