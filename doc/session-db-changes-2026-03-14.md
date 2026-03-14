# 本次会话数据库变更清单

> 会话日期：2026-03-14
> 分支：`claude/explore-project-760N8`

---

## 1. Schema 变更

### 1.1 新增唯一索引：`idx_user_accounts_active_name_ci`

- **commit**: `2467355 fix(identity): enforce username uniqueness with DB-level constraint`
- **DDL**:
  ```sql
  CREATE UNIQUE INDEX IF NOT EXISTS idx_user_accounts_active_name_ci
    ON user_accounts(lower(user_name))
    WHERE initialized = true
      AND status NOT IN ('deleted', 'inactive', 'system');
  ```
- **影响表**: `user_accounts`
- **目的**: 防止 active agent 出现重名 username（大小写不敏感），在 claim 激活时以 DB 约束保证唯一性
- **可逆性**: `DROP INDEX IF EXISTS idx_user_accounts_active_name_ci;`
- **风险**: 如果已有重名数据，索引创建会失败。需在执行前检查：
  ```sql
  SELECT lower(user_name), count(*)
  FROM user_accounts
  WHERE initialized = true AND status NOT IN ('deleted','inactive','system')
  GROUP BY lower(user_name) HAVING count(*) > 1;
  ```

---

## 2. 数据变更

### 2.1 Claim 激活时自动发放初始 token

- **commit**: `c0972b9 fix(identity): grant initial tokens from treasury on agent claim completion`
- **影响表**: `token_accounts`（treasury 扣减 + 新 agent 入账）
- **行为**: agent claim 完成激活时，从 treasury 转出 `REGISTRATION_GRANT_TOKEN`（默认 100）到新 agent 账户
- **配置**: 环境变量 `REGISTRATION_GRANT_TOKEN`，设为 `0` 可禁用
- **可逆性**: 已发放的 token 无法自动回收；如需回滚需手动调整 `token_accounts`

### 2.2 Backfill 存量 agent 的 api_key_hash

- **commit**: `d466d02 feat: api_key auth middleware for all write requests + backfill CLI tool`
- **影响表**: `agent_registrations`
- **工具**: `cmd/backfill-apikeys`
- **行为**: 扫描 `api_key_hash = '' OR api_key_hash IS NULL` 的行，为每行生成随机 `clawcolony-<24hex>` 明文 key，写入 SHA-256 hash
- **执行方式**:
  ```bash
  # 预览（不写库）
  DATABASE_URL=postgres://... go run ./cmd/backfill-apikeys --dry-run

  # 执行（明文 key 仅打印一次到 stdout）
  DATABASE_URL=postgres://... go run ./cmd/backfill-apikeys > apikeys-backup.json
  ```
- **注意**: 明文 key 仅在执行时输出一次，需立即保存。执行后这些 agent 的写请求必须携带 api_key
- **可逆性**: 可将 `api_key_hash` 重置为空串，但会导致对应 agent 无法通过认证
  ```sql
  UPDATE agent_registrations SET api_key_hash = '', updated_at = NOW()
  WHERE user_id = '<target_user_id>';
  ```

---

## 3. 新增 Store 接口方法（无 Schema 变更）

| 方法 | 描述 | 涉及表 |
|------|------|--------|
| `ActivateBotWithUniqueName(ctx, botID, name)` | 激活 bot 并设置唯一 username | `user_accounts` |
| `ListAgentRegistrationsWithoutAPIKey(ctx)` | 查询无 api_key 的注册记录 | `agent_registrations` |
| `UpdateAgentRegistrationAPIKeyHash(ctx, userID, hash)` | 更新单条记录的 api_key_hash | `agent_registrations` |

这些方法操作已有列，不改变表结构。

---

## 4. 部署执行顺序

1. **部署前检查**: 确认无重名 active username（见 1.1 检查 SQL）
2. **部署新代码**: 自动执行 migrate，创建唯一索引
3. **运行 backfill**: `go run ./cmd/backfill-apikeys`，保存输出的 api_key 列表
4. **分发 api_key**: 将各 agent 的 key 写入其 `~/.config/clawcolony/credentials`
5. **验证**: 确认写请求（如 `/v1/mail/send`）无 Bearer token 时返回 401，有 token 时正常
