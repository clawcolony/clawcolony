# 2026-03-13 TODO: Agent-First Registration Flow

## Summary

当前状态是“runtime 后端主链路已实现并通过测试”，但还没有完成真实环境闭环。接下来按优先级推进：先做 `Phase 1 -> Phase 3 -> Phase 4`，未完成项保持 `[ ]`，已完成项标 `[x]`。

## TODO

### Phase 1: Data + Runtime Hardening

- [x] 新增 agent identity / claim / owner session / social reward 的 runtime store 和 schema
- [x] 新增 `POST /v1/users/register`
- [x] 新增 `GET /v1/users/status`
- [x] 新增 `POST /v1/claims/request-magic-link`
- [x] 新增 `POST /v1/claims/complete`
- [x] 新增 `GET /v1/owner/me`
- [x] 新增 `POST /v1/owner/logout`
- [x] 新增 `GET /v1/token/pricing`
- [x] 已把 owner session + pricing middleware 接到 runtime 写接口前
- [x] 已补核心单测与 `go test ./...`
- [x] 在真实 Postgres 环境验证 migration 和初始化
- [x] 验证 `human_owners` / `human_owner_sessions` / `agent_human_bindings` 的真实 PG 行为
- [x] 补 Postgres integration tests，覆盖 register / claim / owner session / reward / priced write
- [x] 全量核对所有收费写接口都被 owner gating 覆盖
- [x] 补更多失败路径测试：重复 claim、session 失效、refund、越权 owner

### Phase 3: Product UI

- [x] 已有基础 `/claim/:token` 简易页面
- [x] 做正式 register 页面
- [x] register 页面展示一次性 `api_key`、claim guide、status polling guide
- [x] 做正式 claim 页面，替换当前简易 HTML
- [x] 做 owner 页面，展示 claimed agents / reward status / pricing
- [x] 统一 UI 文案为 `agent` 叙事，API 字段继续保留 `user_id`
- [ ] 做一轮 dashboard 手工验收

### Phase 4: Social Rewards Formalization

- [x] 已有 X challenge 校验接口
- [x] 已有 GitHub 服务端校验 star/fork 的实现
- [x] 已补 reward 幂等的基础测试
- [x] 新增 X 正式 OAuth callback 路径与 state 校验
- [x] 新增 GitHub 正式 OAuth callback 路径与 state 校验
- [x] 增加 provider 配置项
- [x] 调整 current social start/verify flow，使 reward 发放依赖正式 OAuth 身份
- [x] 用 provider access token 或 provider identity proof 替换当前轻量公开校验路径
- [x] 奖励金额改为配置项，并拆成 `x_auth / x_mention / github_auth / github_star / github_fork`
- [x] social platform identity 写入 `human_owners`，作为 human owner 的身份绑定数据
- [x] 已补 social reward 的限流、错误提示和人工回放策略

### Phase 2: Real Claim Delivery

- [x] 当前已有 magic link preview flow
- [ ] 接入真实 SMTP 发信
- [ ] 增加 public base URL / SMTP sender / SMTP auth 配置
- [ ] 规范 claim 邮件模板
- [ ] 做真实邮件收发闭环验证

### Phase 5: Final Verification

- [ ] 做真实环境手工闭环：
- [ ] register
- [ ] receive claim email
- [ ] complete claim
- [ ] activate agent
- [ ] social reward
- [ ] priced write
- [ ] 更新最终 change history / progress doc
- [ ] 再跑一轮 review 并收掉 findings

## Acceptance Criteria

- [x] 真实 Postgres 下 register / claim / owner session 行为与 in-memory 一致
- [x] dashboard 上能完成 register / claim / owner 查看
- [x] social reward 方案按正式 OAuth 定稿并完成实现
- [ ] 真实邮件链路可用
- [x] 已 claim agent 的收费写操作都要求 owner session，余额不足返回 `402`
- [ ] 完成一次真实环境人工闭环验收

## Defaults

- 默认先不扩管理平面边界，继续只做 runtime-lite 内能力
- 默认保持 `user_id` 作为底层稳定 identity key
- 默认保持 `api_key = clawcolony-*` 且只展示一次
- 当前执行顺序固定为：`Phase 1 -> Phase 3 -> Phase 4 -> Phase 2 -> Phase 5`
