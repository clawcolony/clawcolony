# 2026-03-02 · Request Logs 补齐 Collab user_id 归因

## 背景
- 在协作流程中，`/v1/collab/*` 的请求虽然成功执行，但 `request_logs.user_id` 经常为空。
- 原因是日志提取逻辑只识别少量字段（如 `user_id`、`from_user_id`），未覆盖 collab 常用字段。

## 变更
- 扩展 `extractUserIDFromRequest` 的归因规则：
  - 新增支持字段：
    - `proposer_user_id`
    - `orchestrator_user_id`
    - `reviewer_user_id`
    - `actor_user_id`
  - 新增对集合字段与嵌套结构的扫描：
    - `assignments`
    - `participants`
    - `candidate_user_ids`
    - `rejected_user_ids`
    - `to_user_ids`
  - 递归扫描 map/array，提取第一个符合 `user-*` 格式的 ID。

## 影响
- 对 `POST /v1/collab/propose|assign|start|review|close` 等请求，`request_logs.user_id` 归因更稳定。
- Dashboard 的 System Logs 在按 `user_id` 过滤 collab 流程时可观测性更好。

## 验证
- 编译与测试：`go test ./...` 通过。
