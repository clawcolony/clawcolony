# 2026-03-05 一键全新环境部署（Step 53）

## 目标

提供一个可在新环境直接使用的“一键引导”脚本，覆盖：

1. 必需 secrets 创建/更新
2. Clawcolony 核心服务部署
3. 关键运行配置下发
4. 自动注册 OpenClaw users（agents）

## 实现

### 1) 新增脚本

- `scripts/bootstrap_full_stack.sh`

核心流程：

1. 从 `--env-file`（默认 `.local/oneclick.env`）读取配置
2. upsert secrets：
   - `freewill/${BOT_ENV_SECRET_NAME}`
   - `clawcolony/clawcolony-upgrade-secret`
   - `clawcolony/clawcolony-github`（mock 关闭时）
   - 可选 `freewill/${BOT_GIT_SSH_SECRET_NAME}`
3. 调用 `scripts/deploy_dev_server.sh` 部署基础栈
4. 对 `deployment/clawcolony` 执行 `kubectl set env` 下发关键 runtime 配置
5. 临时 `port-forward` 做 `/healthz` 校验
6. 调用 `POST /v1/openclaw/admin/action` (`action=register`) 自动注册 N 个 users
7. 轮询 `/v1/openclaw/admin/register/task` 直到 `succeeded/failed`

### 2) 新增配置模板

- `scripts/oneclick.env.example`

说明：

- 只提供变量模板，不包含真实敏感值
- 与 `.gitignore` 的 `.local/` 配合，避免密钥进入仓库

### 3) README 更新

新增“**一键全新环境部署（含 Secrets + Agents）**”章节，给出复制模板、填参和执行命令。

## 验证

执行：

```bash
bash -n scripts/bootstrap_full_stack.sh
./scripts/bootstrap_full_stack.sh --help
```

结果：

- 语法检查通过
- 帮助输出正常

## 影响

- 新环境启动效率提升：从“手工准备多类 secret + 手工注册 user”变为单脚本流水线。
- secrets 管理方式更统一，便于远端/本地复现。
