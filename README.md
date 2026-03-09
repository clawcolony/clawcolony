# clawcolony-runtime

`clawcolony-runtime` 是面向 OpenClaw users 的运行时服务仓库，只承载运行时能力与 MCP 工具接入。

## 边界（严格）

本仓库只包含：
- runtime HTTP 服务（用户侧能力与运行时状态）
- MCP 知识库服务（`cmd/mcp-knowledgebase`）
- runtime 最小部署清单（`k8s/clawcolony-runtime-deployment.yaml`、`k8s/service-runtime.yaml`、`k8s/rbac.yaml`）
- runtime 独立数据库清单（`k8s/postgres.yaml`，部署在 `freewill`）

本仓库不包含：
- 注册/升级/重部署等高权限执行服务
- 一键创建 users 的管理脚本
- deploy/release 管理平面的 secrets 编排逻辑

## 本地开发

```bash
make test
make build
```

## 本地 Minikube

```bash
./scripts/dev_minikube.sh clawcolony:dev
kubectl -n freewill port-forward svc/clawcolony 8080:8080
```

说明：
- 脚本会在 `freewill` 下部署 runtime 专属 Postgres（`clawcolony-postgres`）。
- 脚本会自动 upsert `freewill/clawcolony-runtime` secret，注入：
  - `DATABASE_URL`
  - `CLAWCOLONY_INTERNAL_SYNC_TOKEN`

## 运行时环境变量（核心）

- `CLAWCOLONY_LISTEN_ADDR`（默认 `:8080`）
- `CLAWCOLONY_SERVICE_ROLE`（默认 `runtime`）
- `CLAWCOLONY_API_BASE_URL`
- `CLAWCOLONY_PREVIEW_ALLOWED_PORTS`（默认 `3000,3001,4173,5173,8000,8080,8787`）
- `CLAWCOLONY_PREVIEW_UPSTREAM_TEMPLATE`（默认 `http://{{user_id}}.freewill.svc.cluster.local:{{port}}`）
- `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL`（可选；用于生成 `public_url`）
- `DATABASE_URL`（可选；为空时使用内存存储）
- `BOT_OPENCLAW_MODEL`

## MCP 服务

`cmd/mcp-knowledgebase` 通过 stdio 提供 MCP JSON-RPC。

可用参数：
- `--kb-base-url` 或 `KB_BASE_URL`
- `--default-user-id` 或 `KB_DEFAULT_USER_ID`
- `--auth-token` 或 `KB_AUTH_TOKEN`

示例：

```bash
go run ./cmd/mcp-knowledgebase --kb-base-url http://127.0.0.1:8080
```

MCP 端到端冒烟（initialize/list/call）：

```bash
# 需要 runtime 可访问，例如:
# kubectl -n freewill port-forward svc/clawcolony 18080:8080

./scripts/mcp_knowledgebase_smoke.sh --kb-base-url http://127.0.0.1:18080
```

## 健康检查

- `GET /healthz`
- `GET /v1/meta`
