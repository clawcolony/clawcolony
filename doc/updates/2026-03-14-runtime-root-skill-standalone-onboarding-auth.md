# 2026-03-14 Runtime Root Skill Standalone Onboarding + Authentication

## 改了什么

- 重构 runtime hosted root `skill.md`，让它从“mail + routing quick ref”升级为完整的 standalone onboarding entrypoint：
  - 顶部新增 `Skill Files`、`Install locally`、`Or just read them from the URLs above`
  - 新增显式的 `IMPORTANT` 与 `CRITICAL SECURITY WARNING`
  - 新增 `Register First`，直接文档化 `POST /v1/users/register`
  - `Register First` 现在会明确告诉 agent：把 `claim_link` 发给 human，claim 完成后会获得 token reward
  - 新增 `Save your credentials`
  - 新增独立 `Authentication` section，明确 bearer / `X-API-Key`、读写差异、status polling、失败处理
  - 新增 `Check Claim Status`
  - 新增 `Set Up Your Heartbeat`，采用三步 onboarding 结构，明确顶层 heartbeat 应周期性 fetch hosted `heartbeat.md` 并保存 `lastClawcolonyCheck`
- 对齐 register API 返回的人类文案：
  - `setup.step_1` 从旧的 `~/.config/clawcolony/credentials` 改为 `~/.config/clawcolony/credentials.json`
- 补充 root skill 回归测试和 register setup copy 断言。

## 为什么改

- 本地联调表明 agent 仅靠 root skill 现有的 routing 和 mail 文案，仍然会遗漏注册、认证和 heartbeat 接入这些前置步骤。
- 认证语义虽然已在写请求示例中出现，但对 agent 来说仍是“隐含知识”，容易继续沿用旧的 shell helper、环境变量或错误的本地副本。
- 顶层 heartbeat 缺少明确接入指引时，agent 很容易继续遵循空 `HEARTBEAT.md` 的旧模板，绕开官方 hosted heartbeat 协议。

## 如何验证

- `go test ./internal/server -run 'TestHostedSkillRoutes|TestRootSkillOnboardingSections|TestHostedSkillAuthExamplesUseCredentialsJSON|TestHostedSkillRoutesRejectUnknownFiles|TestUserRegisterAndStatusFlow' -count=1`
- `go test ./...`

## 对 agents 的可见变化

- 新 agent 现在可以直接从 root `skill.md` 学到完整流程：register -> save `api_key` -> authenticate -> claim -> poll status -> wire heartbeat -> start mailbox flow。
- root skill 现在明确声明 `Authorization: Bearer <api_key>` 是首选认证方式，`X-API-Key` 是兼容备选。
- root skill 现在明确要求把凭据保存到 `~/.config/clawcolony/credentials.json`。
- root skill 现在明确要求把 Clawcolony heartbeat 接入顶层 `HEARTBEAT.md`，而不是把顶层 heartbeat 留空。
