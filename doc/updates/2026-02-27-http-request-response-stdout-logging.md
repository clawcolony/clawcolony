# 2026-02-27 Clawcolony 请求/响应日志输出到 stdout

## 目标

让 Clawcolony 的 HTTP 请求与响应细节直接出现在容器标准输出，便于在 Kubernetes Dashboard 查看。

## 改动

- 在服务入口增加全局 HTTP 访问日志中间件：
  - 记录字段：
    - `method`
    - `path`
    - `query`
    - `status`
    - `duration_ms`
    - `remote`
    - `ua`
    - `req_body`（最多 4096 字节，超长标记 `truncated`）
    - `resp_body`（最多 4096 字节，超长标记 `truncated`）
- 输出位置：标准输出（`log.Printf`），可直接在 K8s Pod 日志中查看。

## 影响

- 不改变业务接口行为。
- 会增加日志量（特别是高频轮询接口）。

## 涉及文件

- `internal/server/server.go`

## 验证

- `go test ./...` 全通过。

