# 2026-03-07 Runtime apply kube sync and bootstrap patch（Step 82）

## 改了什么

- 在 `bot.Manager` 暴露 `BuildRuntimeProfile`，供 server 侧在 apply 时获取完整 profile（skills/docs/openclaw/mcp）。
- 修复 `POST /v1/prompts/templates/apply` 对现有 users 的实际生效链路：
  - 先构建 profile，再执行 kube 同步，再执行 `ApplyRuntimeProfile`。
  - kube 同步新增两步：
    1) upsert `user-*-profile` ConfigMap 的 seed 数据（含 `clawcolony-mcp-*` 插件 manifest/js）。
    2) 补丁 `workspace-bootstrap` init 脚本：
       - `openclaw.json` 改为每次重启强制从 `/seed/openclaw.json` 覆盖；
       - 补齐 `clawcolony-mcp-{knowledgebase,collab,mailbox,token,tools,ganglia,governance}` 扩展落盘逻辑。
  - 当 ConfigMap 内容有变化时，写入 pod-template annotation 触发 rollout，让现有 pods 立即吃到新 seed。
- 处理并发更新风险：
  - ConfigMap upsert 使用冲突/已存在重试。
  - Deployment patch 使用冲突重试，避免并发更新覆盖。

## 为什么改

- 线上验证发现：`templates/apply` + `rollout restart` 后，agent pod 仍保留旧 skills 与旧 MCP 配置。
- 根因：
  1) 当前 runtime 的 `ApplyRuntimeProfile` 在 no-op provisioner 场景无法更新 kube profile seed；
  2) `workspace-bootstrap` 对 `openclaw.json` 使用“仅不存在才复制”逻辑，导致旧文件长期驻留；
  3) init 脚本仅写入 legacy `mcp-knowledgebase`，没有写入 `clawcolony-mcp-*` 全套扩展目录。

## 如何验证

- 单测：
  - `go test ./...` 通过。
  - 新增验证：
    - `runtimeProfileSeedData` 包含 MCP-only 插件 seed keys。
    - `patchWorkspaceBootstrapScriptForMCP` 可补丁旧脚本且重复调用幂等。
- 代码审查：
  - `claude` diff review 结果：`NO_HIGH_FINDINGS`。

## 对 agents 的可见变化

- 对现有 user 执行 `templates/apply` 后，系统会自动同步 profile seed 到 kube，并确保重启后落地 MCP-only 文件。
- 重启后的 `openclaw.json` 与 `extensions/` 会对齐 `clawcolony-mcp-*`，避免“接口显示已更新但 pod 文件仍旧”的偏差。
