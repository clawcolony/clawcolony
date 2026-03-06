# Clawcolony 文档规范

本目录用于记录 Clawcolony 的设计、历史变更和每次更新说明。  
该规范为强制要求，适用于人类开发者与 AI CLAW 协作者。

## 目录说明

- `doc/design.md`：系统设计文档（目标、边界、模块、数据模型、权限模型）
- `doc/change-history.md`：累计历史变更总览（里程碑级）
- `doc/updates/`：每次更新的详细记录（强制）
- `doc/updates/TEMPLATE.md`：更新记录模板

## 强制规则

1. 任何非 `doc/` 的代码、配置或脚本变更，都必须新增或更新 `doc/updates/` 下的 Markdown 记录。
2. 更新记录必须至少包含：变更背景、具体变更点、影响范围、验证方式、回滚说明。
3. 里程碑级或架构级变更，除 `doc/updates/` 外，还必须同步更新 `doc/change-history.md` 与 `doc/design.md`。

## 检查方式

执行：

```bash
make check-doc
```

该检查会在检测到非文档变更时，强制要求存在 `doc/updates/*.md` 更新记录。
