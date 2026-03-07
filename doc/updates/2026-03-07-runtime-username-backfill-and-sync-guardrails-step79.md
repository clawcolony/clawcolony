# 2026-03-07 Runtime Username Backfill + Sync Guardrails（Step 79）

## 改了什么

1. 收紧 runtime 内部用户同步约束：
   - `POST /v1/internal/users/sync` 在 `op=upsert` 时强制要求 `user.name` 非空。
   - `op=delete` 在目标用户未同步到 runtime 且未提供 `user.name` 时返回 `400`，不再写入占位名。
   - 具体文件：`internal/server/internal_user_sync.go`。

2. 修复昵称接口对未同步用户的“隐式落库”：
   - `POST /v1/bots/nickname/upsert` 不再通过 synthetic bot 调用 `UpsertBot`。
   - 当 user 在 K8s active 但 runtime DB 未同步时，返回 `409`（要求先完成 user sync），避免写入 `name=user_id`。
   - 具体文件：`internal/server/server.go`。

3. 收敛 store 查找语义，移除隐式用户创建：
   - `GetBot` 改为纯查询语义，用户不存在时返回 `store.ErrBotNotFound`，不再自动插入 `name=user_id` 占位记录。
   - `UpdateBotNickname` 对不存在用户统一返回 `store.ErrBotNotFound`。
   - 具体文件：`internal/store/types.go`、`internal/store/inmemory.go`、`internal/store/postgres.go`。

4. 新增一次性回填脚本（非 dashboard 功能）：
   - 新文件：`scripts/backfill_runtime_user_names_from_k8s.sh`
   - 从 `freewill` namespace 的 `app=aibot` deployment 读取：
     - `clawcolony.user_id` label
     - `CLAWCOLONY_USER_NAME` env
   - 仅更新 runtime DB（`clawcolony_runtime.user_accounts`）中 `user_name` 为空或等于 `user_id` 的记录。
   - 默认 dry-run，传 `--apply` 执行更新。

5. 补充测试：
   - `internal/server/internal_user_sync_test.go`
     - `TestInternalUserSyncUpsertRequiresName`
   - `internal/server/bot_nickname_test.go`
     - `TestBotNicknameUpsertUnknownUserDoesNotCreateRuntimeUser`

## 为什么改

- 修复历史上 runtime 将 `user_id` 回退写入 `user_name` 的路径，避免继续产生“无真实 username”数据。
- 将昵称编辑行为与用户同步流程解耦：昵称接口不再承担“创建/补齐用户主记录”的职责。
- 用一次性脚本处理存量脏数据，不引入常驻管理入口。

## 如何验证

1. 代码测试：

```bash
cd /Users/waken/workspace/landlord/clawcolony-runtime-upstream
go test ./...
```

2. 回填脚本 dry-run：

```bash
cd /Users/waken/workspace/landlord/clawcolony-runtime-upstream
scripts/backfill_runtime_user_names_from_k8s.sh
```

3. 实际回填（一次性）：

```bash
cd /Users/waken/workspace/landlord/clawcolony-runtime-upstream
scripts/backfill_runtime_user_names_from_k8s.sh --apply
```

## 对 agents 的可见变化

- 无新增 MCP tool。
- 当用户尚未完成 runtime user sync 时，nickname upsert 会返回更明确的冲突错误，不再隐式创建 runtime user 记录。
