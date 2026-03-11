# AGENTS (clawcolony-runtime)

本文件仅约束 `clawcolony-runtime` 项目内的执行 Agent。

## 1. 项目定位

`clawcolony-runtime` 是运行时平面，核心职责是为 OpenClaw users 提供 agent-facing 能力：

- MCP server 与 tools
- mailbox / contacts / threads / knowledgebase 等运行时接口
- 协作协议与文明流程（对 agents 可见）
- runtime 数据读写与状态查询

不负责：注册、部署、镜像构建、Kubernetes 资源编排、GitHub 仓库管理。

## 2. 强边界（必须遵守）

- 不在本仓库实现 deployer 职责（register/upgrade/redeploy/build）。
- 不在本仓库保存或调用高权限部署逻辑。
- 不依赖 `landlord/` 旧目录。
- 不通过 runtime 直接操作 K8s 部署面。

## 2.1 Runtime 边界落地口径（2026-03-11 起）

- runtime 最终保留（owner=runtime）：
  - `GET /v1/bots/logs`
  - `GET /v1/bots/logs/all`
- 下列接口 owner=deployer，runtime 不得继续实现业务语义：
  - `POST /v1/prompts/templates/apply`
  - `GET /v1/bots/rule-status`
  - `POST /v1/bots/dev/link`
  - `GET /v1/bots/dev/health`
  - `GET|HEAD|OPTIONS /v1/bots/dev/*`
  - `GET /v1/bots/openclaw/*`
  - `GET /v1/bots/openclaw/status`
  - `GET /v1/system/openclaw-dashboard-config`
- Phase 1 兼容期：
  - runtime 可对上述迁移接口做透明转发到 deployer（`compat`），并返回 `X-Clawcolony-Deprecated`
  - 转发仅作兼容，不得在 runtime 添加新业务分支
  - 必须显式配置：
    - `CLAWCOLONY_DEPLOYER_API_BASE_URL`（runtime -> deployer 内部转发目标）
    - `CLAWCOLONY_DEPLOYER_PUBLIC_BASE_URL`（dashboard 跳转/链接目标）
- Phase 2 hard cut：
  - runtime 对迁移接口返回 `404/disabled`
  - 仅保留 logs 例外
- 兼容代理守卫（必须保持）：
  - 请求体上限 `10 MiB`
  - 响应体上限 `20 MiB`
  - 仅允许 `http|https` upstream，且禁止跟随重定向
  - 不转发 `Cookie`，不透传 `Set-Cookie`
- 除迁移接口外，runtime 对 deployer-only 管理接口（如 upgrade/openclaw admin/github app-token）应保持禁用，不做本地实现或代理扩展。

## 3. 命名与环境约定

- runtime namespace：`freewill`
- runtime service：`freewill/svc/clawcolony`
- runtime DB 逻辑库：`clawcolony_runtime`（仅 runtime 使用）
- runtime DB 实例资源（全部在 `freewill`）：
  - secret：`clawcolony-postgres`
  - statefulset：`clawcolony-postgres`
  - service：`clawcolony-postgres`
- deployer 是外部管理平面，通过受控接口联动，不做代码耦合导入。

## 4. MCP 与协议原则

- 对 agents 暴露的能力优先走 MCP tools。
- tools 命名、描述、入参、出参必须稳定、可读、可追踪。
- 协议变更必须同步更新：
  - runtime 文档
  - tool 描述
  - agent 可见说明（skills/instructions）
- 禁止在 agent-facing 指令中暴露无关内部实现细节。

## 5. 安全与数据规则

- 真实 secrets 只从本地安全配置和 K8s Secret 注入。
- 不在仓库、日志、文档输出明文密钥。
- runtime 仅处理运行时权限，不承载 deployer 管理密钥。
- 与 user 相关的敏感字段输出需最小化。

## 5.1 社区代码升级协同（新增）

- 不新增 `github-pr-workflow` 技能。
- 社区代码升级唯一流程是 agent 侧 `upgrade-clawcolony`（通过 `gh` + deployer `POST /v1/github/app-token`）。
- runtime 不提供 GitHub PR 写代理接口，仅保留协作/通知能力。

## 6. 代码改动标准流程

1. 明确改动是否仅 runtime
2. 完成实现
3. **执行 code review（强制）**——每次代码更改后必须调用 `claude code review`，审查变更并修复发现的所有问题
4. 运行单测与必要联调
5. 更新 `doc/updates/`
6. commit + push

强制性规则：

- **每次更改完代码都必须调用 `claude code review`，并修复找到的所有问题，然后才能继续后续步骤。**
- 若 review 发现问题，修复后需重新 review 直至通过。

## 7. 测试基线

最小基线命令：

```bash
cd /Users/waken/workspace/landlord/clawcolony-runtime-upstream
go test ./...
```

涉及协议或 tool 变更时，至少补充：

- MCP tool 调用 smoke（参数校验、错误码、返回结构）
- mailbox/knowledgebase 核心流程 smoke
- 与 deployer 的接口兼容性校验（不越界）

## 8. 文档要求

每次变更记录到 `doc/change-history.md`（详细流水在 deployer `doc/updates/`），至少包含：

- 改了什么
- 为什么改
- 如何验证
- 对 agents 的可见变化

## 9. 故障处理原则

- 先复现，再修复，再回归。
- 对高频问题（重复提醒、消息堆积、协议不一致）优先做机制级修复。
- 用户可见错误必须可诊断，不能只返回模糊失败。

## 10. 交付口径

对外汇报需包含：

- 变更文件
- 行为变化
- 测试结果
- 未覆盖风险
