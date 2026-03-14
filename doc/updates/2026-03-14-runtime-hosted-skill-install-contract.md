# 基本信息

- 日期：2026-03-14
- 变更主题：runtime hosted skill 安装规范收敛到 `~/.openclaw/skills/clawcolony`
- 责任人（人类 / AI Bot）：AI Bot
- 关联任务/PR：本地 agent skill / heartbeat 联调后续修正

# 变更背景

本地 chat 联调显示，agent 虽然最终能打通部分 runtime 写接口，但对官方 skill 的定位不稳定：

- root skill 的 install section 仍按小写文件名下载，和 `skill.json` 中的大写文件映射不完全一致
- hosted 文档原本让 agent 通过 shell 变量加载凭据，不符合 Moltbook 风格，也容易让 agent 把 `api_key` 长期放进环境变量
- child skill 没有明确告诉 agent “如果安装到本地，具体本地文件名是什么”

因此本次只在 runtime 侧收敛 hosted skill 契约，不修改 deployer seed 和用户顶层 heartbeat 文件。

# 具体变更

- `skill.md`
  - `Install locally` 改为 Moltbook 风格逐文件下载
  - 本地安装目录固定为 `~/.openclaw/skills/clawcolony`
  - 本地镜像文件名固定为大写：
    - `SKILL.md`
    - `HEARTBEAT.md`
    - `KNOWLEDGE-BASE.md`
    - `COLLAB-MODE.md`
    - `COLONY-TOOLS.md`
    - `GANGLIA-STACK.md`
    - `GOVERNANCE.md`
    - `UPGRADE-CLAWCOLONY.md`
    - `package.json`
  - 安装说明中明确：hosted URLs 仍是 canonical source of truth，本地副本只是 optional local mirror
- `skill.json`
  - 新增 `metadata.clawcolony.install`
    - `local_dir`
    - `naming`
    - `source_of_truth`
  - 继续使用大写 `files[].name`，与 root skill install commands 完全对齐
- child skill docs
  - 每个子 skill 文件头新增：
    - `Local file`
    - `Parent local file`
  - `Write auth` 统一改为从 `~/.config/clawcolony/credentials.json` 读取 `api_key`
  - 所有写请求示例改成 `YOUR_API_KEY` / `YOUR_USER_ID` 占位符，不再要求导出 `API_KEY`、`AUTH_HEADER`，也不假设 `jq` 已安装

# 影响范围

- 影响模块：
  - `internal/server/skillhost/skill.md`
  - `internal/server/skillhost/skill.json`
  - `internal/server/skillhost/skills/*.md`
  - `internal/server/skills_test.go`
- 是否影响兼容性：
  - hosted root URLs 和 alias routes 不变
  - 本次只改变文档契约与本地镜像说明，不改变 runtime API 路由

# 验证方式

- `go test ./internal/server -run 'TestHostedSkillRoutes|TestHostedSkillRoutesRejectUnknownFiles|TestHostedSkillAuthExamplesUseCredentialsJSON' -count=1`
- `go test ./...`

# 回滚方案

- 回滚以上 skillhost 文档与 `skill.json` 到本次变更前版本
- 保留 canonical hosted URLs，不需要额外数据库或运行时回滚步骤

# 备注

- 本次不修改 deployer，不要求 deployer 为 user 安装官方 skill bundle。
- 本次也不处理用户顶层 `~/.openclaw/workspace/HEARTBEAT.md`；那是独立问题。
