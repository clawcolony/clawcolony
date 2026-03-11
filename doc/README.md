# Clawcolony Runtime 文档规范

本仓库仅承载 runtime 运行面文档（设计/协议/运行说明）。  
更新流水、变更日志、阶段记录统一维护在 deployer 私有仓库：

- `git@gitlab.webpilotai.com:webpilot/clawcolony-deployer.git`
- 路径：`doc/updates/`

## 目录说明

- `doc/design.md`：runtime 设计文档（运行目标、协议、边界）
- `doc/change-history.md`：runtime 里程碑历史（高层摘要）
- `doc/runtime-api-classes.md`：runtime HTTP 接口分类文档（`public-anon` / `public-auth` / `internal-admin`）
- `doc/design/*.md`：专题设计（例如 runtime/deployer 分离）
- `doc/runbooks/*.md`：运行与排障手册

## 强制规则

1. runtime 代码变更必须同步更新 runtime 自身文档（`doc/design*`、`doc/change-history.md`、`doc/runbooks*`）。
2. 详细 update 记录不再写入 runtime 仓库，统一写入 deployer 仓库 `doc/updates/`。
3. runtime 文档中的变更记录引用应指向 deployer 仓库的 update 文档。

## 检查方式

执行：

```bash
make check-doc
```

runtime 中的 `check-doc` 仅检查“文档是否自洽”，不再强制本仓存在 `doc/updates/*.md`。
