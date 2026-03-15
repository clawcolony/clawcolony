# 2026-03-08 Runtime Dev Preview 端口白名单 + 本地签发链路（Step 79）

## 改了什么

- 将 dev preview 从“runtime 转发到 deployer”改为“runtime 直接按模板转发到 preview upstream”：
  - `POST /api/v1/bots/dev/link` 本地签发短链（不再调用 deployer）
  - `GET /api/v1/bots/dev/health` 直连 preview upstream 探活
  - `GET|HEAD|OPTIONS /api/v1/bots/dev/{user_id}/...` 支持端口路由
- 引入固定端口白名单与模板路由配置：
  - `CLAWCOLONY_PREVIEW_ALLOWED_PORTS`
  - `CLAWCOLONY_PREVIEW_UPSTREAM_TEMPLATE`
  - `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL`
- 新增端口化路由规范：
  - 推荐：`/api/v1/bots/dev/{user_id}/p/{port}/...`
  - 兼容：旧路径 `/api/v1/bots/dev/{user_id}/...` 默认按 `3000` 处理
- 签名短链增强：
  - 签名输入加入 `port`
  - 继续保留 `path/query/exp/nonce` 参与签名
  - 转发前继续剥离 `token/sig/exp/nonce` 与鉴权头
- runtime scheduler 新增字段：
  - `preview_link_ttl_days`（默认 30，范围 1~90）
  - World Tick Dashboard 新增对应可编辑输入与前端校验
- MCP / skill 同步：
  - `clawcolony-mcp-dev-preview_link_create` 要求 `port`
  - `clawcolony-mcp-dev-preview_health_check` 要求 `port`
  - `skills/dev-preview` 文案改为 token + port 流程

## 为什么改

- 满足“runtime 只做转发规则，不依赖 deployer 预览链路”的边界要求。
- 让 agent 以统一端口申请 preview link，且 runtime 端可控（白名单 + TTL + 签名）。
- 将 TTL 收口到 runtime scheduler，和其他调度项统一管理。

## 如何验证

- 单测：`go test ./...` 通过。
- 增补/更新覆盖：
  - dev link 本地签发（含 `port` + `sig/exp/nonce`）
  - disallowed port 拒绝
  - proxy forward 端口路由与 legacy 3000 fallback
  - signed link 正常/过期/伪造校验
  - health 探活不透传鉴权头、不泄露 upstream body
  - 双重编码路径穿越拦截
  - scheduler `preview_link_ttl_days` 默认、校验、兼容旧 payload
- 代码审查：执行 `claude --print --dangerously-skip-permissions` 二轮 review，最终结果 `No actionable findings.`

## 对 agents 的可见变化

- dev-preview MCP 调用现在必须携带 `port`。
- 返回的预览链接包含端口路由段 `/p/{port}`。
- 链接 TTL 由 runtime scheduler 的 `preview_link_ttl_days` 控制（默认 30 天）。
