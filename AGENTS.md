# AGENTS (clawcolony-runtime)

本文件仅约束 `clawcolony-runtime` 项目内的执行 Agent。

## 1. 项目定位

`clawcolony-runtime` 是运行时平面，核心职责是为 OpenClaw users 提供 agent-facing 能力：

- MCP server 与 tools
- mailbox / contacts / threads / knowledgebase 等运行时接口
- 协作协议与文明流程（对 agents 可见）
- runtime 数据读写与状态查询

不负责：注册、部署、镜像构建、Kubernetes 资源编排、GitHub 仓库管理等管理平面职责。

## 2. 强边界（必须遵守）

- 不在本仓库实现管理平面职责（register/upgrade/redeploy/build）。
- 不在本仓库保存或调用高权限部署逻辑。
- 不依赖 `landlord/` 旧目录。
- 不通过 runtime 直接操作 K8s 部署面。

## 2.1 Runtime 边界落地口径（2026-03-12 起）

- runtime 为 runtime-lite：只保留 agent 社区模拟/MCP 核心能力，不再承担 deployment/dev/openclaw/chat/prompt/pod logs 相关职责。
- runtime 不允许对 removed domains 做兼容代理；这些路径在 runtime 必须稳定返回 `404`。
- runtime 下列接口必须 hard cut（`404`）且不得恢复：
  - `/v1/prompts/templates`
  - `/v1/prompts/templates/upsert`
  - `/v1/prompts/templates/apply`
  - `/v1/bots/logs`
  - `/v1/bots/logs/all`
  - `/v1/bots/rule-status`
  - `/v1/bots/dev/link`
  - `/v1/bots/dev/health`
  - `/v1/bots/dev/*`
  - `/v1/bots/openclaw/*`
  - `/v1/bots/openclaw/status`
  - `/v1/system/openclaw-dashboard-config`
  - `/v1/chat/send`
  - `/v1/chat/history`
  - `/v1/chat/stream`
  - `/v1/chat/state`
- runtime 仍保留身份接口：
  - `GET /v1/bots`
  - `POST /v1/bots/nickname/upsert`
  - 且仅允许 DB 视角，不得依赖 K8s active set。
- runtime dashboard 边界：
  - 移除 Chat、User Logs、Prompts、OpenClaw/Dev 入口与页面。
  - `/dashboard` 与当前主导航只保留 runtime-lite 社区页面：
    - `mail`
    - `collab`
    - `kb`
    - `governance`
    - `world-tick`
  - 下列 runtime 观测页当前仍可路由访问，但不属于主导航核心页面：
    - `system-logs`
    - `ops`
    - `monitor`
    - `world-replay`
    - `ganglia`
    - `bounty`
- 数据边界（runtime 侧）：
  - `user_accounts.user_id` 必须保留，不可删除。
  - 不再在 runtime `user_accounts` 持久化 `gateway_token`、`upgrade_token`。
  - removed domains 已从 runtime 移除（chat/prompt/register/upgrade）。
  - runtime schema 收缩（drop removed 表/列）是受控动作：
    - 默认关闭；
    - 仅在设置 `CLAWCOLONY_RUNTIME_SCHEMA_SHRINK=1` 时执行 destructive shrink。

## 2.2 Removed Domains 切换与数据规则（强制）

- runtime schema shrink 前必须先完成 removed domains 的一次性导出、迁移与校验：
  - 源：runtime removed domains
  - 目标：新的 owner 数据域（overwrite）
  - 至少校验：表行数、主键范围/序列、`user_accounts` 投影一致性
- 禁止在未完成导入校验前开启 `CLAWCOLONY_RUNTIME_SCHEMA_SHRINK=1`。
- 线上 split 环境（2026-03-12）已完成 hard cut + schema shrink：
  - 不得再假设 runtime 数据库保留 `chat_messages`、`prompt_templates`、`register_tasks`、`register_task_steps`、`upgrade_audits`、`upgrade_steps`
  - 不得再假设 runtime `user_accounts` 保留 `gateway_token`、`upgrade_token`

## 3. 命名与环境约定

- runtime namespace：`freewill`
- runtime service：`freewill/svc/clawcolony`
- runtime DB 逻辑库：`clawcolony_runtime`（仅 runtime 使用）
- runtime DB 实例资源（全部在 `freewill`）：
  - secret：`clawcolony-postgres`
  - statefulset：`clawcolony-postgres`
  - service：`clawcolony-postgres`
- 线上远端端口转发约定（`~/bin/clawcolony_pf`）：
  - `35512` = runtime dashboard / API
  - `35513` = minikube dashboard

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
- runtime 仅处理运行时权限，不承载管理平面密钥。
- 与 user 相关的敏感字段输出需最小化。

## 5.1 社区代码升级协同（新增）

- 不新增 `github-pr-workflow` 技能。
- 社区代码升级唯一流程是 agent 侧 `upgrade-clawcolony`。
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
- 边界一致性校验（不越界、不恢复 removed domains）

## 8. 文档要求

每次变更记录到 `doc/change-history.md`，至少包含：

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
