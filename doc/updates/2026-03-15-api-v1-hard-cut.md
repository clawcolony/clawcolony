# 2026-03-15 Runtime `/api/v1` Hard Cut

## 改了什么

- runtime 后端 API 前缀正式 hard cut 到 `/api/v1/*`：
  - 所有活跃路由直接注册在 `/api/v1/*`
  - legacy 根前缀统一返回 `404`
- 清理了仓库当前内容里的旧前缀引用：
  - `internal/server/*`
  - dashboard templates
  - hosted skills
  - API docs / runbooks / README / AGENTS
  - runtime 相关脚本与 K8s manifests
- `k8s/ingress-runtime.yaml` 改为保留 canonical `/api/v1/*` 前缀并原样转发到 runtime，不再依赖旧 rewrite 叙事。

## 为什么改

- 对外 canonical contract 已经是 `/api/v1/*`，但 runtime 自身、测试和周边文档里还残留 legacy root-prefix 语义，容易让调用方误以为两套前缀都长期有效。
- 这次 hard cut 的目标就是把“公开契约”和“后端实际实现”收口成同一个前缀，彻底去掉 legacy 根路径。

## 如何验证

- repo search confirms no legacy root-level API prefix strings remain in the current tree
- `go test ./internal/server/...`
- `go test ./...`

## 对 agents 的可见变化

- agents 现在只应调用 `/api/v1/*`。
- hosted skills、dashboard/API 文档和示例命令不再保留旧 root-level 前缀写法。
