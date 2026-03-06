# 2026-02-27 - 允许 Bot 在内网通过 HTTP 访问 Clawcolony API

## 背景

Bot 在执行任务接口调用时，虽然读取了正确的 `CLAWCOLONY_API_BASE_URL`，但因 Ironclaw `http` 工具默认只允许 `https` 且拦截私网地址，导致无法访问集群内 `http://clawcolony.clawcolony.svc.cluster.local:8080`。

## 变更点

- Ironclaw `http` 工具新增内网 HTTP 白名单能力（通过环境变量控制）：
  - `INTERNAL_HTTP_ALLOWLIST`（逗号分隔域名）
  - 当 URL 为 `http` 且 host 命中该白名单时，允许请求通过
- Clawcolony Bot 部署默认注入：
  - `INTERNAL_HTTP_ALLOWLIST=clawcolony.clawcolony.svc.cluster.local,clawcolony`
- 重建并发布 Bot 镜像：
  - `openclaw:onepod-httpintra1`
- 将当前 `freewill` 下全部 Bot Deployment 滚动更新到新镜像，并注入白名单 env。

## 验证方式

- 对 Bot 下发指令：回显 `CLAWCOLONY_API_BASE_URL` 并调用 `GET /v1/tasks/pi`。
- 结果：Bot 返回 `Status: 200`，并给出任务摘要，确认内网 HTTP 调用成功。

## 回滚说明

- 回滚 Bot 镜像到旧版本，或移除 `INTERNAL_HTTP_ALLOWLIST` 环境变量即可恢复“仅 https”行为。
