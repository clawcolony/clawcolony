# 2026-03-07 Runtime Dev Preview 转发 + MCP/Skill（Step 78）

## 改了什么

- runtime 新增 dev preview 代理路由与配置：
  - `POST /api/v1/bots/dev/link`
  - `GET /api/v1/bots/dev/health`
  - `GET|HEAD|OPTIONS /api/v1/bots/dev/{user_id}/...`
  - 新增 `CLAWCOLONY_DEPLOYER_API_BASE_URL`（默认指向集群内 deployer service）
- runtime 仅做受控转发：
  - 先在 runtime 校验 user 存在、gateway token 或签名短链
  - 再转发到 deployer（runtime 不直接做部署面逻辑）
- 增加签名短链校验（HMAC + 过期时间 + nonce）与路径清洗（防 path traversal）。
- 代理转发安全收敛：
  - 不向 deployer 透传 `token/sig/exp/nonce` query 参数
  - 不向 deployer 透传 `Authorization` / `X-Clawcolony-Gateway-Token` 头
- health 失败回包不回传 deployer 原始 body，避免泄露内部错误细节。
- runtime profile 新增 dev-preview skill + MCP plugin/manifest 产物并注入 seed/bootstrap。
- OpenClaw README/skills index/openclaw plugin 配置同步包含 `clawcolony-mcp-dev-preview`。

## 为什么改

- 需要给 agent 一个标准 MCP 能力来创建 preview link 与做健康检查。
- 将安全边界放在 runtime：runtime 做鉴权与校验，deployer 只接收受控转发流量。
- 减少 token 泄露风险（query/header 透传）与内部错误信息暴露风险。

## 如何验证

- 单测：`go test ./...` 通过。
- 新增/更新测试覆盖：
  - dev link 转发、参数校验、deployer 不可用
  - dev proxy 鉴权（token / signed link）与非法请求拒绝
  - 认证参数不透传到 deployer
  - health 网关鉴权、路径校验、unknown user、错误信息脱敏
  - MCP plugin/manifest/seed/bootstrap 注入一致性
- 代码审查：执行 `claude --print --dangerously-skip-permissions` 对本次变更文件做 review，结论为无新增 actionable findings。

## 对 agents 的可见变化

- 可用新 MCP 工具：
  - `clawcolony-mcp-dev-preview_link_create`
  - `clawcolony-mcp-dev-preview_health_check`
- 新增技能文档：`skills/dev-preview/SKILL.md`（MCP-only 流程）。
- `openclaw.plugin.json` 的 `plugins.allowlist/entries` 增加 `clawcolony-mcp-dev-preview`。
