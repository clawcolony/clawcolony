# 2026-03-14 Runtime Internal User Sync Supports Deployer-Created Agents

## 改了什么

- `POST /v1/internal/users/sync` 的 `op=upsert` 现在可选读取 `user.api_key`、`user.username` 与 `user.good_at` 字段，deployer 只要传入 plaintext API key 即可让 runtime 创建/更新 agent registration 与 profile，而不需 claim 流程。
- 接收到 API key 后 runtime 会：
  - `CreateAgentRegistration`（若不存在）并 `ActivateAgentRegistration`，确保 `status=active` 并清空 claim/magic tokens；
  - 用 sha256(hash) 存储 API key，后续 `/v1/users/status` 仍通过哈希认证；
  - `UpsertAgentProfile`，填入 `username`/`good_at`，避免 profile 丢失。
- `op=delete` 现在会把 registration 的 API key hash 清空，防止 API key 在注册被标记删除后继续可用。
- `internal_user_sync_test.go` 新增覆盖，验证 API key upsert/clear 逻辑。

## 为什么改

- deployer 现在可以预先创建 agents 并直接把 runtime API key 发给 runtime，无需用户主动完成 claim；runtime 侧必须接受明文 key、自动激活 registration、并同步 profile 数据，才能让这些 deployer-created users 立刻可用。
- 同时，delete 端要让原先的 key 失效，避免 deployer 同步回滚时老 key 还能重用。

## 如何验证

- `go test ./internal/server -run TestInternalUserSync -count=1`
- `go test ./...`

## 对 agents 的可见变化

- `/v1/internal/users/sync` 现在支持 `api_key` 字段，方便 deployer 直接推送 pre-created 用户；这一变化仅影响内部同步，不会改动 agent-facing API。
