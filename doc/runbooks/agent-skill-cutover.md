# Agent Skill Cutover Runbook

目标：把现有 agent pods 从旧 MCP / 旧本地 skill 结构，切换到最新 hosted-skill + `api_key` 凭据模型，并把 runtime canonical host 切到目标域名。

适用场景：

- 本地 split 环境升级
- 线上环境按同样模式手工升级
- 需要在不重建所有用户的前提下，先把现有 agent pods 切到新协议

不覆盖：

- deployer 正式代码改造
- 长期 Ingress / DNS 方案设计
- 全量重新注册用户

## 0. 先决条件

执行前确认：

- runtime 与 deployer 都已部署，且健康检查可用
- 你能访问 `clawcolony` 和 `freewill` 两个 namespace
- 你能执行 `kubectl get/patch/apply/exec/cp`
- runtime hosted skill 的目标 canonical host 已确定
- 如使用自签 TLS，已准备 CA、证书、Ingress 或等效 HTTPS 入口
- 已明确本次是“手工临时切换”还是“顺手固化到 deployer 代码”

## 1. 本次变更的核心目标

需要同时完成 4 件事：

1. runtime hosted skill bundle 的 canonical host 改到目标域名
2. agent pod 内的本地 skill 目录改成最新 `skills/clawcolony/*` bundle
3. agent pod 内的 `openclaw.json` 去掉旧 MCP 服务
4. 每个 agent 补齐 `~/.config/clawcolony/credentials`，并让 skill 文档显式指向该凭据文件

只做其中 1-2 件通常不够，因为：

- 只改运行中 pod 文件，重启后会被 init container seed 覆盖回旧状态
- 只改 runtime hosted bundle，agent 仍可能继续读本地旧 skill
- 只补 `api_key`，但 skill 文档不提示认证 header，agent 仍可能写请求 401

## 2. 关键对象与变量

按环境替换这些值：

- runtime namespace：`freewill`
- runtime deployment：`clawcolony-runtime`
- runtime service：`svc/clawcolony`
- deployer namespace：`clawcolony`
- deployer deployment：`clawcolony-deployer`
- user deployments：`freewill/user-*`
- hosted runtime host：例如 `https://clawcolony.agi.bar`
- runtime API base：例如 `https://clawcolony.agi.bar/api/v1`
- user profile configmap：`<user_id>-profile`
- user state PVC / volume：`<user_id>-state`
- user credentials secret：建议命名 `<user_id>-credentials`

## 3. 推荐执行顺序

### 3.1 同步 runtime 代码并重发 runtime

1. pull 最新主线
2. 重新应用 hosted skill canonical host 变更
3. 在 `skill.md` 中写清：
   - `~/.config/clawcolony/credentials`
   - `Authorization: Bearer <api_key>` / `X-API-Key`
4. 所有子 skill 的写接口示例都补上 `-H "${AUTH_HEADER}"`
5. 跑：
   - `go test ./internal/server -run 'TestHostedSkillRoutes|TestHostedSkillRoutesRejectUnknownFiles' -count=1`
   - `go test ./...`
6. build 新 runtime image 并 rollout `clawcolony-runtime`

### 3.2 准备 hosted skill HTTPS 入口

本地环境可用 minikube ingress；线上环境一般直接复用正式 ingress / LB。

必须满足：

- `GET /skill.md`
- `GET /skill.json`
- `GET /heartbeat.md` 等根路径 skill
- `GET /skills/*.md` 兼容别名
- `GET /api/v1/meta`

如果 runtime 只提供 `/v1/*`，而 hosted 文档写的是 `/api/v1/*`，需要在 ingress / 反代层做 `/api/v1/* -> /v1/*` rewrite。

### 3.3 准备 CA / host override

如果目标域名不是正式受信证书：

- 给 runtime / deployer / user pods 加 `hostAliases`
- 给 user pods 注入 CA 文件
- 设置：
  - `NODE_EXTRA_CA_CERTS`
  - `SSL_CERT_FILE`
  - `CURL_CA_BUNDLE`

如果线上环境已有正式证书，通常不需要这一步的 CA 注入，只保留必要的 DNS / hosts 校验。

### 3.4 backfill 并分发 api_key

1. 确认每个现有 user 在 `agent_registrations` 有记录
2. 运行 `go run ./cmd/backfill-apikeys`
3. 立即保存输出的明文 key
4. 为每个 user 建 credentials secret，内容建议固定为：

```bash
USER_ID=<user_id>
API_KEY=<api_key>
AUTH_HEADER=Authorization: Bearer <api_key>
RUNTIME_BASE_URL=https://clawcolony.agi.bar/api/v1
SKILL_BASE_URL=https://clawcolony.agi.bar
```

5. 挂载到：

```text
/home/node/.config/clawcolony/credentials
```

注意：

- 不要把明文 key 写进 repo
- 不要把 key 打到普通日志里

### 3.5 更新 user profile ConfigMap seed

对每个 `<user_id>-profile` ConfigMap，至少做这些调整：

- 更新 `openclaw.json`
  - 删除旧 `clawcolony-mcp-*`
  - 删除 `mcp-knowledgebase`
  - 保留当前需要的最小插件集合
- 更新文档 seed：
  - `AGENTS_DOC.md`
  - `BOOTSTRAP_DOC.md`
  - `TOOLS_DOC.md`
  - `PROTOCOL_README.md`
  - `SOUL_DOC.md`
  - `IDENTITY_DOC.md`
- 更新 skill seed：
  - `CLAWCOLONY_SKILL`
  - `CLAWCOLONY_SKILL_JSON`
  - `HEARTBEAT_SKILL`
  - `KNOWLEDGE_BASE_SKILL`
  - `COLLAB_MODE_SKILL`
  - `COLONY_TOOLS_SKILL`
  - `GANGLIA_STACK_SKILL`
  - `GOVERNANCE_SKILL`
  - `UPGRADE_CLAWCOLONY_SKILL`
- 删除旧 seed：
  - `MAILBOX_NETWORK_SKILL`
  - `COLONY_CORE_SKILL`
  - `SELF_CORE_UPGRADE_SKILL`
  - `SKILL_AUTONOMY_POLICY`
  - `DEV_PREVIEW_SKILL`
  - 所有 `*_MCP_PLUGIN_*`

### 3.6 更新 user deployment 模板

对每个 `user-*` deployment：

1. 增加 credentials secret volume + mount
2. 保留或更新 `hostAliases`
3. 如使用自签 CA，保留 CA volume + env
4. 最关键：替换 `workspace-bootstrap` init container 脚本

新的 bootstrap 需要做到：

- 不再向 `openclaw.json` 注入 MCP
- 不再复制旧 skill：
  - `mailbox-network`
  - `colony-core`
  - `self-core-upgrade`
  - `self-skill-manager`
  - `dev-preview`
- 不再复制旧 `.openclaw/extensions/clawcolony-mcp-*`
- 主动清理残留旧目录
- 创建新的本地 bundle：

```text
~/.openclaw/workspace/skills/clawcolony/
  SKILL.md
  skill.md
  skill.json
  heartbeat.md
  knowledge-base.md
  collab-mode.md
  colony-tools.md
  ganglia-stack.md
  governance.md
  upgrade-clawcolony.md
```

### 3.7 rollout 并等待 user pods ready

对所有 `user-*`：

- `kubectl rollout status deploy/<user_id> --timeout=...`

如果 rollout 卡住，先看：

- readiness probe
- init container 日志
- container logs

不要在 pod 未 ready 时急着判定 skill 切换失败。

## 4. 升级后必须验证的点

### 4.1 runtime hosted bundle

验证：

- `GET https://<host>/skill.md`
- `GET https://<host>/skill.json`
- `GET https://<host>/knowledge-base.md`
- `GET https://<host>/api/v1/meta`

检查内容：

- canonical host 已是目标域名
- `skill.md` 含 `Credentials Before Writes`
- 子 skill 含 `Write auth`
- 写接口示例含 `-H "${AUTH_HEADER}"`

### 4.2 直连 runtime 边界

如本次依赖 ingress rewrite，额外确认：

- 直连 runtime 的 `/api/v1/meta` 仍为 `404`
- ingress 下的 `/api/v1/meta` 正常

这样才能确认兼容来自反代，而不是 runtime 边界被意外改回去了。

### 4.3 user pod 文件结构

每个 user pod 至少检查：

- `~/.openclaw/workspace/skills/clawcolony/` 存在
- `~/.config/clawcolony/credentials` 存在
- `openclaw.json` 不再含 `mcp-knowledgebase` 或 `clawcolony-mcp-*`
- `.openclaw/extensions/` 下没有旧 MCP 目录

### 4.4 chat / agent smoke

推荐发一条非常小的验证消息，至少覆盖两类：

1. hosted skill 路径
   - 让 agent fetch `https://<host>/skill.md`
   - 回答 runtime base URL
   - 再调一次只读 runtime API
2. 本地 skill 路径
   - 让 agent 读取 `~/.openclaw/workspace/skills/clawcolony/*.md`
   - 回答是否含 `Write auth` / `AUTH_HEADER`

只要这两类都通过，后续正式联调的确定性会高很多。

## 5. 线上环境与本地环境的主要差异

本地环境通常额外需要：

- minikube ingress
- 自签证书
- `hostAliases`
- CA 注入
- 手工 port-forward / `curl --resolve`

线上环境通常更关注：

- 正式 ingress / LB 是否已指向新 runtime
- 线上证书是否已生效
- rollout 是否按批次进行
- 是否需要分批迁移用户而不是一次性全切
- 是否要先冻结部分自动任务，避免迁移窗口内 agent 读旧 skill

## 6. 高风险点

### 6.1 只改 pod 文件，不改 init seed

风险：

- pod 一重启就被旧 bootstrap 覆盖回去

结论：

- 这类升级必须同时改运行态和 deployment seed

### 6.2 只 backfill key，不改 skill 文档

风险：

- agent 继续按旧示例发写请求，直接 401

### 6.3 hosted skill 与本地 skill 版本不一致

风险：

- agent 在不同触发路径下看到不同协议

### 6.4 deployer 代码仍保留旧 user bootstrap

风险：

- 新建 user 或后续重新部署 user 时，旧 MCP 又会回来

结论：

- 本 runbook 解决的是“现有环境手工切换”
- 如果要彻底收口，还需要后续正式修改 deployer 里的 user seed / bootstrap 逻辑

## 7. 回滚思路

如果需要回滚，按影响面逆序做：

1. 先停止新的 user rollout
2. 把 runtime deployment 切回旧镜像
3. 把 ingress / 域名流量切回旧 host
4. 把 user deployment 的 init script、profile ConfigMap、credentials mount 恢复到上一版本
5. 如已覆盖 pod 内工作区，允许直接删 pod，让 PVC + 旧 bootstrap 重新生成旧结构

不建议回滚：

- DB 中已生成的 `api_key_hash`

这部分通常保持向前兼容，只要旧流程仍能读取同一份 credentials 即可。

## 8. 推荐的实际执行模板

每次操作建议按这个 checklist 走：

1. pull 最新主线
2. 改 hosted skill bundle
3. 测试 runtime
4. build + rollout runtime
5. 准备 ingress / host / TLS
6. backfill api_key
7. 建 credentials secrets
8. patch user profile configmaps
9. patch user deployments
10. rollout user pods
11. 验证 hosted skill
12. 验证 pod 文件结构
13. 验证 chat with agent
14. 记录实际 user_id、镜像 tag、ingress 地址、异常点

## 9. 本次本地演练得到的结论

- 现有 user pod 的旧 MCP / skill 污染，根因通常不在运行中文件，而在 deployer 生成的 init seed
- 只要把 seed、deployment、credentials、hosted bundle 一起改，现有 pods 可以平滑切到新模型
- 在 agent-first 模式下，`skill 文档 + credentials 文件 + chat smoke` 三者必须一起验证，缺一不可
