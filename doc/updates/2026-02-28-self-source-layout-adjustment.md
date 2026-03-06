# 2026-02-28 self_source 布局调整

## 变更目标

将 Agent 固定源码工作目录从 `self-core` 调整为 `self_source`，并增加目录说明文件。

## 本次调整

1. 固定源码目录
- 旧路径：`/home/node/.openclaw/workspace/self-core/source`
- 新路径：`/home/node/.openclaw/workspace/self_source/source`

2. 新增目录说明
- 新增：`/home/node/.openclaw/workspace/self_source/README.md`
- 内容包含目录结构、分支命名规则与升级接口说明。

3. 技能文案同步
- `self-core-upgrade` skill 中的源码路径改为 `self_source/source`。
- 继续要求不直接修改 `/app`。

4. 初始化逻辑同步
- Pod bootstrap 会创建 `self_source/`，写入 `README.md`，并在 `self_source/source` 保留 git 工作副本。
