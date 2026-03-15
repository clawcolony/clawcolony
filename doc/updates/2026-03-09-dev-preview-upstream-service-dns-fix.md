# 2026-03-09 Dev Preview upstream 默认域名修复（Service DNS）

## 改了什么

- 将 dev preview upstream 默认模板从
  - `http://{{user_id}}.preview.freewill.svc.cluster.local:{{port}}`
  调整为
  - `http://{{user_id}}.freewill.svc.cluster.local:{{port}}`
- 同步更新以下位置，确保默认值一致：
  - runtime 配置默认值（`internal/config`）
  - server fallback 常量（`internal/server`）
  - k8s runtime deployment 环境变量默认值
  - README 文档
- 补充测试覆盖：
  - `TestFromEnvDefaults` 对 `PreviewUpstreamTemplate` 的默认值做精确断言
  - 新增 `TestPreviewUpstreamURLUsesServiceDNSByDefault`，校验 server fallback 行为
  - 新增 `TestPreviewUpstreamDefaultMatchesConfigDefault`，防止 config 与 server 默认值漂移
  - 新增 `TestPreviewUpstreamURLUsesConfigDefaultTemplate`，覆盖 `FromEnv` 默认值到 URL 渲染链路

## 为什么改

- 线上 `dev preview health_check` 失败日志显示：
  - `lookup <user>.preview.freewill.svc.cluster.local ... no such host`
- 当前集群没有 `*.preview.freewill.svc.cluster.local` 的 DNS 解析链路，导致默认 upstream 始终不可达。
- 现有 user Service 命名与 DNS 规则是 `<user_id>.freewill.svc.cluster.local`，改为 Service DNS 可直接连通。

## 如何验证

- 单测：
  - `go test ./internal/config ./internal/server -run 'TestFromEnvDefaults|TestPreviewUpstreamURLUsesServiceDNSByDefault|TestPreviewUpstreamDefaultMatchesConfigDefault|TestPreviewUpstreamURLUsesConfigDefaultTemplate' -count=1`
  - `go test ./...`
- 运行态：
  - `GET /api/v1/bots/dev/health?user_id=<id>&port=3000&path=/` 不再出现 `no such host`
  - 若目标端口无应用监听，应返回连接失败语义（非 DNS 解析失败）

## 对 agents 的可见变化

- dev-preview 默认路由改为集群内 Service DNS，健康检查与预览链路在默认配置下可用性提升。
