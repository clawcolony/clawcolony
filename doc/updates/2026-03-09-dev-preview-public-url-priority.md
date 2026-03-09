# 2026-03-09 Dev Preview 链接返回优先级与用途说明修复

## 改了什么

- 强化 agent 可见的 dev-preview 规则（`BuildDevPreviewSkillMCPOnly` / `TOOLS`）：
  - 明确对外返回顺序：`public_url > absolute_url > relative_url`
  - 明确三类 URL 用途：
    - `public_url`：给终端用户直接打开（首选）
    - `absolute_url`：同网络内联调/排障
    - `relative_url`：同域系统内跳转
  - 明确禁止对终端用户返回 `*.svc.cluster.local`、`localhost/127.0.0.1/0.0.0.0`
  - 健康检查失败时要求回报可诊断原因（例如 `connection refused` / `no such host`），禁止继续发失效链接
- 补充测试：
  - `TestBuildDevPreviewSkillMCPOnlyEnforcesNoLocalURLFallback` 增加 `public_url` 优先级与用途断言
  - 新增 `TestBotDevLinkProxyIncludesPublicURLWhenConfigured`，验证 `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL` 生效后响应包含 `public_url`

## 为什么改

- 线上出现 agent 返回集群内域名（`.svc.cluster.local`）给用户，导致“链接生成成功但用户无法直接打开”。
- 需要在 agent 层明确“对外可用链接”优先级，避免误把内部链路字段当用户入口。

## 如何验证

- 单测：
  - `go test ./internal/bot ./internal/server -run 'TestBuildDevPreviewSkillMCPOnlyEnforcesNoLocalURLFallback|TestBotDevLinkProxyIncludesPublicURLWhenConfigured' -count=1`
  - `go test ./...`
- 运行态（需配置 `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL`）：
  - 调用 `POST /v1/bots/dev/link`，确认响应含 `item.public_url`
  - agent 对外回复优先使用 `public_url`，不再给 `.svc.cluster.local` 链接

## 对 agents 的可见变化

- 预览链接返回行为更清晰：优先给可外部访问的 `public_url`，并对其他 URL 的用途给出明确边界，减少误返内网地址。
