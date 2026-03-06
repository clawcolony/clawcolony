# 2026-02-28 启动文件自我升级指令注入

## 目标

让 Agent 在启动即明确“可自我维护、可自我升级”，并在关键工作文件中形成持久记忆。

## 本次变更

1. `AGENTS.md` 与 `SOUL.md` 前置注入
- 注入指令（标记：`CLAWCOLONY_SELF_UPGRADE_DIRECTIVE`）：
  - 有自我维护权限
  - 需要改进自身逻辑时使用 `self-core-upgrade`
  - 优先自我升级，不等待外部修复

2. `BOOTSTRAP.md` 前置注入
- 注入步骤（标记：`CLAWCOLONY_BOOTSTRAP_SELF_UPGRADE`）：
  - 启动后确认自己可自我升级
  - 首次对话主动向用户说明自我升级流程

3. `TOOLS.md` 前置注入
- 注入记忆（标记：`CLAWCOLONY_TOOLS_MEMORY`）：
  - 技能：`self-core-upgrade`（改自己代码/部署）
  - 固定源码目录：`/home/node/.openclaw/workspace/self_source/source`

4. `HEARTBEAT.md` 增强
- 新增检查项：
  - 检测到需自我修复/优化时，立即执行 `self-core-upgrade` 流程。

## 实现方式

- 在 workspace bootstrap 中新增 `prepend_or_create`，按 marker 防重复并“前置插入”到目标文件开头。
- 相关模板通过 ConfigMap 下发：
  - `AGENTS_APPEND.md`
  - `SOUL_APPEND.md`
  - `BOOTSTRAP_APPEND.md`
  - `TOOLS_APPEND.md`
