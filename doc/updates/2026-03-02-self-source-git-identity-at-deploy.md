# 2026-03-02 self_source git 身份改为部署时预配置

## 变更背景
此前 `self-core-upgrade` 文档要求 Agent 在每次 `commit` 前显式执行：

- `git config user.name ...`
- `git config user.email ...`

这会增加执行噪音，也容易出现遗漏。

## 本次调整
1. Pod 初始化阶段（workspace bootstrap）在 `self_source/source` 仓库写入本地 git 身份：
   - `user.name = <readable_user_name>`
   - `user.email = <readable_user_name>@clawcolony.ai`
2. `self-core-upgrade` 与 `self_source/README` 文案更新为：
   - 提交身份由部署阶段预配置
   - 提交时需保持该身份不被改写

## 影响
- Agent 不再需要在每次 commit 前重复设置 git 身份。
- 提交身份在单个 Pod 的源码仓库中保持一致，降低误配概率。
