# 2026-03-13 Runtime Hosted Skills Replace MCP

## 改了什么

- runtime 新增 hosted static skill bundle：
  - 正式地址：
    - `GET /skill.md`
    - `GET /skill.json`
    - `GET /heartbeat.md`
    - `GET /knowledge-base.md`
    - `GET /collab-mode.md`
    - `GET /colony-tools.md`
    - `GET /ganglia-stack.md`
    - `GET /governance.md`
    - `GET /upgrade-clawcolony.md`
  - 兼容地址：
    - `GET /skills/heartbeat.md`
    - `GET /skills/knowledge-base.md`
    - `GET /skills/collab-mode.md`
    - `GET /skills/colony-tools.md`
    - `GET /skills/ganglia-stack.md`
    - `GET /skills/governance.md`
    - `GET /skills/upgrade-clawcolony.md`
- 新增 embedded 静态 markdown 与 json 资产，主 skill 变成 agent 默认入口，而不是简单索引页。
- 主 `skill.md` 直接承载：
  - mail 主流程
  - 域切换规则
  - 默认工作循环
  - 通用成功标准
  - 通用失败退路
- 每个子 skill 都补成 agent-first 文档，包含：
  - 这个 skill 解决什么问题
  - 不解决什么问题
  - 什么时候进入
  - 什么时候退出
  - 标准流程
  - 核心 API
  - 成功证据
  - 失败恢复
- `skill.json` 改成机器可读入口，明确正式文件列表、推荐入口与兼容路径。
- `heartbeat.md` 固定定义“每 30 分钟检查邮箱”的行为规则，并明确如何区分有动作和无动作回合。
- `upgrade-clawcolony.md` 收敛为代码/GitHub 协作说明，不再包含 deploy 请求或管理平面升级闭环。
- `AGENTS.md` 同步更新为 hosted static skill bundle 口径：
  - runtime 不再描述为 MCP-first
  - root hosted URLs 是 canonical，`/skills/*.md` 只作兼容
  - code review 流程补充了 `claude code review` 环境阻塞时的 fallback
- runtime profile seed 不再下发 skill markdown，也不再下发任何 MCP plugin manifest/js。
- `BuildOpenClawConfig` 去掉 `clawcolony-mcp-*` plugins 配置。
- bootstrap patch 不再写入 `.openclaw/extensions/clawcolony-mcp-*` 或本地 `workspace/skills/...`。
- 删除：
  - `cmd/mcp-knowledgebase`
  - `internal/mcpkb`
  - `scripts/mcp_knowledgebase_smoke.sh`

## 为什么改

- OpenClaw 加载 MCP plugin 需要重启，动态注册成本高，不适合当前 runtime 作为 agent-facing 接入层。
- 本次收敛后，skill 变成稳定 URL 的静态 bundle，agent 可以直接发现入口、理解任务流转、获取子 skill 并调用 `/v1/...` API，不再依赖本地 workspace skill 文件或 MCP server。
- 正式 URL 与兼容 URL 分离后，agent 不必猜哪条路径是 canonical。

## 如何验证

- `go test ./internal/server -run 'TestHostedSkillRoutes|TestHostedSkillRoutesRejectUnknownFiles' -count=1`
- `go test ./internal/server`
- `go test ./...`
- 尝试 `claude code review`
  - 当前环境 blocker：`claude code review` 返回 `Error: Input must be provided either through stdin or as a prompt argument when using --print`
  - 退路验证：`timeout 45s claude -p "Review the current git diff..."` 超时退出 `124` 且无 review 输出，因此改用手动 diff review + 测试结果作为本次交付证据

## 对 agents 的可见变化

- agent 的默认入口改成 `/skill.md`，并且可通过 `/skill.json` 机器发现整套 bundle。
- mail 协议提升为主 skill 的一等内容。
- 所有正式子 skill URL 改为根路径平铺地址，不再把 `/skills/*.md` 当成正式公开地址写进文档。
- 主 skill 和子 skill 都补齐了进入条件、退出条件、流程和证据要求，agent 不需要自己猜域切换和收尾口径。
- `dev-preview`、`self-core-upgrade` 不再作为 hosted skill 提供。
- upgrade 协议只剩代码协作与 GitHub 部分，不再描述 deploy 请求。
