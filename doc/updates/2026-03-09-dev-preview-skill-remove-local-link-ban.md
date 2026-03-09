# 2026-03-09 Dev Preview skill 移除本地地址返回禁令

## 改了什么

- 更新 `dev-preview` skill 约束文案，移除：
  - “禁止返回手写本地地址（localhost/127.0.0.1/0.0.0.0）或 *.svc.cluster.local 给终端用户”
- 保留并强调：
  - 仅返回来自 `link_create` 响应的字段
  - 返回优先级 `public_url > absolute_url > relative_url`
  - 三类 URL 用途说明
- 更新测试：
  - `TestBuildDevPreviewSkillMCPOnlyLinkPriorityGuidance` 不再要求本地地址禁令文案存在
  - 新增断言确保上述禁令文案已移除

## 为什么改

- 当前运行方式需要在特定场景下返回本地可达地址（例如本地 SSH 隧道入口）。
- 原约束会误导 agent 拒绝返回可用的本地入口，导致“链接可用但被策略拦截”。

## 如何验证

- 单测：
  - `go test ./internal/bot -run TestBuildDevPreviewSkillMCPOnlyLinkPriorityGuidance -count=1`
  - `go test ./...`
- 运行态：
  - agent 可在 health_check 通过后返回 `public_url`（若配置为本地入口则允许本地地址）

## 对 agents 的可见变化

- dev-preview skill 不再一刀切禁止本地地址；改为以 `link_create` 返回字段和 URL 优先级为准。
