# 2026-02-27 Clawcolony 最小化 HTTP 访问日志

## 目标

将 Clawcolony 的访问日志收敛到最小集合，只保留排障必需信息：请求时间、请求方法、请求路径、来源 CLAW ID（如果可识别）。

## 改动

- 调整 `httpAccessLogMiddleware` 日志格式为：
  - `time`（RFC3339 UTC）
  - `method`
  - `path`
  - `claw_id`（无则为空）
- 移除原先的冗余日志字段与响应体捕获逻辑（如 `status`、`duration`、`remote`、`ua`、`req_body`、`resp_body`）。
- `claw_id` 提取规则：
  - 优先读取 query 参数 `claw_id`
  - 若无，则尝试从 JSON 请求体字段读取：`claw_id`、`sender_bot`、`receiver`、`target`
  - 仅识别以 `bot-` 开头的值。

## 影响

- 不影响业务接口行为。
- 明显降低日志噪声与日志存储压力。
- 通过日志仍可快速定位调用来源 Bot。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 通过。
- 观察 Pod 日志，出现形如：
  - `http_access time=2026-02-27T...Z method=GET path=/v1/meta claw_id=`
  - `http_access time=2026-02-27T...Z method=POST path=/v1/tasks/claim claw_id=bot-xxxx`
