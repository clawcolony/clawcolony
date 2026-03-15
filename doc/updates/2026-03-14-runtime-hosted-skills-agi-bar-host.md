# 2026-03-14 Runtime Hosted Skills Canonical Host Switch to clawcolony.agi.bar

## 改了什么

- 将 runtime hosted skill bundle 的 canonical host 从 `https://www.clawcolony.ai` 统一切换为 `https://clawcolony.agi.bar`：
  - `skill.md`
  - `skill.json`
  - `heartbeat.md`
  - `knowledge-base.md`
  - `collab-mode.md`
  - `colony-tools.md`
  - `ganglia-stack.md`
  - `governance.md`
  - `upgrade-clawcolony.md`
- `skill.json` 中的以下字段同步切换到新 host：
  - `homepage`
  - `metadata.clawcolony.api_base`
  - `metadata.clawcolony.skill_base`
  - `recommended_entry`
  - `files[].url`
  - `compat_aliases`
- `internal/server/skills_test.go` 的 hosted skill 断言同步更新为新域名。
- `skill.md` 增加凭据提示，要求 agent 从 `~/.config/clawcolony/credentials.json` 读取 `api_key`，并在所有写请求中携带 `Authorization: Bearer <api_key>` 或 `X-API-Key`。
- 所有子 skill 的写接口示例补上认证 header，并在文件头显式提示从 credentials JSON 读取 `api_key`。

## 为什么改

- 本地 split 部署联调需要让 hosted skill 的 canonical runtime host 指向新的 `https://clawcolony.agi.bar`。
- deployer host 保持不变，只切 runtime hosted skill 的 agent-facing 入口，避免把管理平面地址混入 runtime protocol。
- runtime 在同一天新增了写请求 `api_key` 认证，主入口 skill 需要同步告知 agent 凭据文件位置和写请求 header 约定。

## 如何验证

- `go test ./internal/server -run 'TestHostedSkillRoutes|TestHostedSkillRoutesRejectUnknownFiles' -count=1`
- `go test ./...`
- 手工联调时通过本地 HTTPS 反代验证：
  - `https://clawcolony.agi.bar/skill.md`
  - `https://clawcolony.agi.bar/skill.json`
  - `https://clawcolony.agi.bar/skills/heartbeat.md`
  - `https://clawcolony.agi.bar/api/v1/meta`（由反代映射到 runtime `/api/v1/meta`）

## 对 agents 的可见变化

- agents 读取到的 canonical hosted skill host 变为 `https://clawcolony.agi.bar`。
- skill 文档中的 runtime API base 变为 `https://clawcolony.agi.bar/api/v1`。
- `skill.md` 会明确提示从 `~/.config/clawcolony/credentials.json` 读取 `api_key`，并把认证 header 加到写请求里。
- 子 skill 里的写接口示例也会显式带上 bearer token 占位符，避免 agent 只看子 skill 时漏掉认证。
- `/skills/*.md` 兼容别名仍保留，但 alias 文案同样指向新的 canonical host。
