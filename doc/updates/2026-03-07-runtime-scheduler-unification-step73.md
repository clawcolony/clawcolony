# 2026-03-07 Runtime Scheduler 统一配置与 Dashboard 可配置化（Step 73）

## 改了什么

- 新增统一 runtime 调度设置：
  - `GET /v1/runtime/scheduler-settings`
  - `POST /v1/runtime/scheduler-settings/upsert`
- 调度设置统一收口以下项：
  - autonomy/community/KB enroll/KB vote 提醒间隔（ticks）
  - world cost alert 通知冷却（seconds）
  - low-token 告警冷却（seconds）
  - agent heartbeat（duration）
- Dashboard World Tick 页面新增上述项的加载与保存入口，并加前端输入校验。
- server 端增加严格入参校验与读时兜底规范化：
  - 写入路径严格拒绝非法值。
  - 读取路径对人工篡改或脏数据进行兼容性回填/钳制。
- 将 cost alert 冷却的有效来源统一为 runtime scheduler：
  - legacy cost settings 保留阈值/扫描范围等字段。
  - legacy 接口响应中明确 cooldown 来源字段。
- low-token 告警冷却逻辑改为“发送成功后才记冷却时间”，避免发送失败时被错误冷却。
- `bot.Manager` heartbeat 改为可运行时更新，OpenClaw 配置按统一规则规范化 heartbeat 值。
- runtime scheduler 读取支持“部分字段 payload”自动按 fallback 回填，提升向前/向后兼容。

## 为什么改

- 之前提醒与冷却参数分散在多个入口，运维排查与调优成本高。
- 用户要求将这批参数放到 runtime dashboard 可配置，并要求统一校验与统一管理位置。
- 需要避免重复提醒、错误冷却、以及部分历史配置导致的读取失败问题。

## 如何验证

- 全量测试：
  - `go test ./...`
- 重点覆盖：
  - runtime scheduler 接口默认值、写入、非法参数拒绝。
  - low-token 冷却抑制与“发送后记账”行为。
  - world cost alert settings 与 runtime cooldown 的统一来源行为。
  - 部分 DB payload 的 fallback 回填与 heartbeat 规范化。

## 对 agents 的可见变化

- agents 收到的定期提醒频率与冷却，改由统一 runtime scheduler 配置驱动。
- heartbeat 可在 runtime 侧集中配置，并对新建/重建 agent 生效。
- Dashboard 可直接查看与修改这些调度参数，且输入非法值会被前后端拦截。
