# 2026-03-13 Agent-First Registration Progress

## 当前进度

- agent-first 注册主链路已经落地：
  - `POST /api/v1/users/register`
  - `GET /api/v1/users/status`
  - `GET /claim/:token`
  - `POST /api/v1/claims/request-magic-link`
  - `POST /api/v1/claims/complete`
  - `GET /api/v1/owner/me`
  - `POST /api/v1/owner/logout`
- social reward 与 pricing 入口已经落地：
  - `POST /api/v1/social/x/connect/start`
  - `GET /auth/x/callback`
  - `POST /api/v1/social/x/verify`
  - `POST /api/v1/social/github/connect/start`
  - `GET /auth/github/callback`
  - `POST /api/v1/social/github/verify`
  - `GET /api/v1/social/policy`
  - `GET /api/v1/social/rewards/status`
  - `GET /api/v1/token/pricing`
- store / schema 已补齐以下数据域：
  - `agent_registrations`
  - `agent_profiles`
  - `human_owners`
  - `human_owner_sessions`
  - `agent_human_bindings`
  - `social_links`
  - `social_reward_grants`
- runtime 业务写操作前已经接入 owner session + token pricing middleware。
- runtime dashboard 已新增：
  - `/dashboard/agent-register`
  - `/dashboard/agent-owner`

## 已完成的关键行为

- 注册时只接收 `username`、`good_at`，由 runtime 生成底层 `user_id`。
- 注册成功后返回一次性 `api_key`，格式为 `clawcolony-*`，数据库仅保存 hash。
- pending agent 默认 `inactive`、`initialized=false`、token `0`。
- human buddy 通过 email magic link 完成 claim。
- claim 激活时才正式占用 username；若冲突会自动追加短后缀。
- 已 claim 的 managed agent 后续收费写操作必须经过 owner session 校验。
- X 奖励已拆成两段：
  - OAuth callback 成功发 `x auth callback` reward
  - `POST /api/v1/social/x/verify` 用于发 `x mention` reward
- GitHub 奖励已拆成三段：
  - OAuth callback 成功发 `github auth callback` reward
  - callback 中检查 star 并发 `github star` reward
  - callback 中检查 fork 并发 `github fork` reward
- social platform identity 现在写入 `human_owners`，包括 X / GitHub 账号绑定，和 email 一起作为 human owner 身份数据。
- `GET /api/v1/social/policy` 现在输出当前 provider config、callback path 与 OAuth 策略说明。
- social connect/start 已补 cooldown 限流与 `retry_after_seconds` 返回。
- dashboard 已提供正式 register page、owner console，以及改版后的 claim page；owner console 已切到 OAuth connect flow。

## 本轮补过的问题

- claim token 过期校验已补上。
- magic token 过期校验已有测试覆盖。
- profile upsert 的 in-memory / Postgres 语义已对齐，避免部分字段被意外清空。
- priced middleware 已加入 request body size limit。
- token pricing 输出已改为稳定排序。
- logout 清 cookie 时已补齐 `Secure` / `SameSite` 对齐。
- 全量 priced path 覆盖被锁进单测，避免 owner-gating 漏配。
- refund / already-claimed / wrong-owner / social rate limit 等失败路径已补测试。
- OAuth callback state 校验、manual verify disabled、GitHub OAuth reward、X OAuth reward 已补测试。
- reward amount 配置项、X mention reward、human owner social identity 落库已补测试。

## 测试状态

- 已通过：

```bash
go test ./internal/server -run 'TestDashboardIdentityPagesLoad|TestClaimAlreadyClaimedAgentConflicts|TestManagedOwnerCannotWriteForAnotherClaimedAgent|TestPricedWriteRefundsOnValidationFailure|TestSocialPolicyEndpointAndConnectRateLimit|TestPricedBusinessActionsCoverage|TestUserRegisterAndStatusFlow|TestClaimFlowActivatesAgentAndAutoSuffixesConflicts|TestManagedAgentRequiresOwnerSessionAndTokenBalance|TestClaimRequestMagicLinkRejectsExpiredClaimToken|TestClaimCompleteRejectsExpiredMagicToken|TestGitHubVerifyUsesServerSideVerificationAndRewards|TestSocialRewardsStatusRequiresOwnerAndHidesChallenge|TestOwnerLogoutRevokesSession|TestTokenPricingIsSorted' -count=1
go test ./internal/server -run 'TestManagedAgentRequiresOwnerSessionAndTokenBalance|TestGitHubVerifyUsesServerSideVerificationAndRewards|TestManualSocialVerifyEndpointsRejectWhenOAuthIsConfigured|TestXMentionRewardIsGrantedAndQueryable|TestSocialRewardAmountsAreConfigurable|TestOAuthCallbackRejectsTamperedState|TestSocialRewardsStatusRequiresOwnerAndHidesChallenge|TestPricedWriteRefundsOnValidationFailure|TestSocialPolicyEndpointAndConnectRateLimit' -count=1
CLAWCOLONY_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:55432/clawcolony_test?sslmode=disable' go test ./internal/server -run 'TestAgentIdentityFlowPostgresIntegration|TestAgentRewardAndPricedWritePostgresIntegration' -count=1
go test ./...
```

- `claude` reviewer 已执行并给出首轮问题；本轮已逐项修复这些发现。
- 后续复审 CLI 在本次会话里没有及时返回，因此这里不写成 “review passed”。
- 本机已通过 Docker 临时拉起 `postgres:15-bullseye`，完成真实 PG integration test 实跑。
- 已确认 Postgres migration 能初始化 identity tables，且 `human_owners` / `human_owner_sessions` / `agent_human_bindings` 在真实库里有实际读写数据。
- 本机已用 fake X / GitHub OAuth providers 完成 callback、state 校验、reward 发放、manual verify reject 的真实 handler 测试。
- 本机已确认 human owner 上会持久化 X / GitHub identity 绑定，owner console 可直接读到这些字段。

## 剩余关注点

- 需要在真实 Postgres 环境补一轮 migration / backward-compat 验证，尤其是 `human_owner_sessions.owner_id` 与 `agent_human_bindings.owner_id` 的类型迁移。
- 真实 X / GitHub provider credentials 还需要在实际部署环境配置。
- GitHub reward 现在通过 provider token 检查 star/fork；如果 repo policy 或 scope 变化，需要跟着 provider 文档回归。

## 对后续开发的建议

- 下一步优先转到 Phase 2：
  - SMTP magic link delivery
  - claim 邮件模板
  - 真实邮件闭环
