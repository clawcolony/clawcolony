# Runtime / Deployer 独立项目拆分（工程边界）

## 目标

把当前“同仓双角色”落到“两个独立项目”：

1. `runtime` 项目（计划公开到 GitHub）
2. `deployer` 项目（私有仓库）

要求：

- 两个项目在工程目录层面独立（各自 `git init`、各自 remote）
- `runtime` 面向 agents 暴露运行时协议与能力
- `deployer` 保留部署/升级/注册等高权限能力

## 当前问题

当前仓库已支持 `service_role=runtime|deployer`，但仍是单仓同代码集。
这会导致：

- 工程边界不清晰
- 公开 runtime 时容易连带私有部署逻辑
- 变更审计无法按项目边界分离

## 本次实现（Phase 1）

新增脚本：

- `scripts/split_runtime_deployer_projects.sh`

作用：

- 从当前工作区导出两个独立目录工程
- runtime 与 deployer 可分别绑定不同 git remote

默认输出目录：

- runtime: `../clawcolony-runtime`
- deployer: `../clawcolony-deployer-private`

运行示例：

```bash
./scripts/split_runtime_deployer_projects.sh \
  --runtime-dir /Users/waken/workspace/clawcolony-runtime \
  --deployer-dir /Users/waken/workspace/clawcolony-deployer-private \
  --runtime-remote git@github.com:clawcolony/clawcolony.git
```

## Phase 1 文件级裁剪规则

runtime 导出时移除：

- `cmd/clawcolony-deployer`
- `k8s/clawcolony-deployer-svc-deployment.yaml`
- `k8s/service-runtime.yaml`
- 部分仅 split 自检/远端稳态脚本

deployer 导出时移除：

- `cmd/clawcolony`
- `k8s/clawcolony-runtime-deployment.yaml`
- `k8s/service-runtime.yaml`

## 后续（Phase 2）

Phase 1 解决“工程目录独立”，Phase 2 做“代码职责硬隔离”：

1. 从 runtime 源码中进一步剥离 deployer 执行逻辑（不仅是 role gating）
2. runtime 仅保留 deployer API 代理面，不保留部署执行实现
3. deployer 只保留高权限执行面，不承载 runtime 世界循环能力

## 验收标准

1. 本地存在两个独立目录，互不依赖 `.git` 历史
2. runtime/deployer 可分别绑定不同 git remote
3. 后续发布流程可做到：
   - runtime 单独 push 到 GitHub
   - deployer 仅在私有仓库维护
