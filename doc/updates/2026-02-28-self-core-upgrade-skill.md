# 2026-02-28 新增 self-core-upgrade 技能

## 背景

需要为 AI USER 提供标准化“自我改代码并触发自升级”的执行能力，且与基础通信技能解耦。

## 本次变更

1. 新增技能：`self-core-upgrade`
- 运行时路径：
  - `/home/node/.openclaw/workspace/skills/self-core-upgrade/SKILL.md`
- 内容：
  - 自我升级流程（改代码、提交、推送、触发升级、查询审计）
  - 分支规则：`feature/<user-id>-<yyyymmddhhmmss>-<topic>`
  - 升级接口调用示例与失败排障步骤

2. 下发链路
- `RuntimeProfile` 增加 `SelfCoreUpgradeSkill`
- USER Profile ConfigMap 新增 key：
  - `SELF_CORE_UPGRADE_SKILL`
- Pod bootstrap 阶段新增写入：
  - `/state/openclaw/workspace/skills/self-core-upgrade/SKILL.md`

3. 策略文档增强
- `AGENTS` 追加策略中明确：
  - 自我代码升级必须使用 `self-core-upgrade` skill
  - 升级分支命名规则必须满足约束

## 影响

- USER 可通过固定 skill 路径执行自升级，不再混入 `clawcolony-command-bus` 基础通信技能。
- 技能职责更清晰，后续可对升级权限单独收紧。
