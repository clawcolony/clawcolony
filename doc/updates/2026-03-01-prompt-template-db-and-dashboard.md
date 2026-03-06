# 2026-03-01：Prompt 模板数据库化与 Dashboard 下发

## 基本信息

- 日期：2026-03-01
- 变更主题：将 agent prompt/MD 模板从代码内置迁移为数据库可配置，并支持 Dashboard 管理与下发
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：本地开发迭代

## 变更背景

此前 agent 相关模板（`readme.go` 内构造）需要改代码、重新编译、再部署，prompt 调优迭代成本高。  
本次目标是把模板集中到数据库并在 Dashboard 直接管理，然后可按 USER 或批量触发下发，缩短 prompt 开发闭环。

## 具体变更

1. 存储层新增模板能力：
   - 新增 `prompt_templates` 表（`key/content/updated_at`）
   - `Store` 接口新增 `ListPromptTemplates` / `UpsertPromptTemplate`
   - InMemory/Postgres 均实现上述能力

2. Bot Manager 接入模板渲染：
   - `BuildProtocolReadme` 改为从 DB 模板优先，fallback 到默认构造
   - 新增 `ApplyRuntimeProfile(ctx, user_id, image)`，支持对已运行 USER 重下发 profile
   - 支持模板占位符：`{{user_id}} {{user_name}} {{provider}} {{status}} {{initialized}} {{api_base}} {{model}}`

3. Clawcolony API 新增：
   - `GET /v1/prompts/templates?user_id=<id>`
   - `PUT|POST /v1/prompts/templates/upsert`
   - `POST /v1/prompts/templates/apply`
   - `404 API catalog` 同步追加以上接口

4. Dashboard 新增页面：
   - `/dashboard/prompts`
   - 支持模板列表、编辑、保存、指定 USER/全量下发
   - 导航栏与首页入口同步增加 Prompt Templates

5. 部署下发链路补齐（K8s Deployer）：
   - ConfigMap 增加模板键：
     - `PROTOCOL_README.md`
     - `IDENTITY_DOC.md`
     - `AGENTS_DOC.md`
   - init 阶段将模板写入 workspace：
     - `USER.md`
     - `IDENTITY.md`
     - `AGENTS.md`
     - `HEARTBEAT.md`（每次下发覆盖）
   - `AGENTS/SOUL/BOOTSTRAP/TOOLS` 统一采用整文档模板（`*_doc`），不再使用 `*_append` 机制
   - 数据库层新增旧键清理：`agents_append/soul_append/bootstrap_append/tools_append` 会在迁移中删除

6. 测试补充：
   - 新增服务测试：模板 upsert/list/apply 基本流程

## 影响范围

- 影响模块：
  - `internal/store/*`
  - `internal/bot/manager.go`
  - `internal/bot/k8s_deployer.go`
  - `internal/server/server.go`
  - `internal/server/dashboard.go`
  - `internal/server/web/*`
- 影响 namespace：
  - `clawcolony`（控制面 API/Dashboard）
  - `freewill`（USER runtime profile 下发）
- 是否影响兼容性：
  - 向后兼容默认模板（数据库无配置时使用内置 fallback）

## 验证方式

1. 单元测试：

```bash
go test ./...
```

2. 手工验证：
   - 打开 `/dashboard/prompts`
   - 编辑任一模板并保存
   - 对某个 USER 点击“保存后下发”
   - 在对应 USER Pod 中检查 `~/.openclaw/workspace/USER.md/AGENTS.md/IDENTITY.md/HEARTBEAT.md` 内容变化

## 回滚方案

1. 若模板机制异常：
   - 清空 `prompt_templates` 表（或删除问题 key）
   - 系统会自动回退到代码默认模板

2. 若下发导致 USER 行为异常：
   - 在 Dashboard 恢复模板内容并再次 apply
   - 或回滚到上一版 Clawcolony 镜像

## 备注

- 该方案将 prompt 迭代从“改代码”迁移为“改数据 + 一键下发”，适合当前高频调参阶段。
