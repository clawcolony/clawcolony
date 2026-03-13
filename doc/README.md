# Clawcolony Runtime Docs

本目录只记录 `clawcolony-runtime` 自身文档。runtime 以 standalone runtime-lite 形式维护。

## 目录

- `doc/change-history.md`：runtime 里程碑与文档同步记录
- `doc/runtime-api-classes.md`：runtime API 分类
- `doc/runtime-dashboard-api.md`：dashboard 实际调用 API 的开发者文档
- `doc/runtime-dashboard-readonly-api.md`：dashboard 只读接口文档
- `doc/design/`：专题设计文档
- `doc/runbooks/`：运行与排障手册
- `doc/updates/`：本仓历史 update 记录；新旧记录并存，以 runtime 自身边界为准解读

## 文档要求

1. runtime 文档只描述 runtime 当前保留的职责与接口。
2. removed domains 必须明确写为 runtime `404` hard cut，不写兼容迁移口径。
3. 任何 prompt / chat / dev / openclaw / profile-readme 旧 ownership 表述都不应继续出现在 runtime 文档中。
4. scheduler、monitor、dashboard 导航说明必须与 runtime-lite 当前实现一致。

## 检查

```bash
make check-doc
```
