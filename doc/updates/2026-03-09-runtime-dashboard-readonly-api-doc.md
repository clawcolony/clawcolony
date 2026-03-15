# 2026-03-09 Runtime Dashboard ReadOnly API 文档拆分

## 改了什么

- 新增文档：`doc/runtime-dashboard-readonly-api.md`
- 文档范围限定为 dashboard 只读接口：
  - 仅保留 `GET` 与 `GET /api/v1/chat/stream`（SSE）
  - 覆盖 World / Monitor / Bots / Chat / System / Mail / Token / Bounty / Collab / KB / Governance / Ganglia / Prompts
  - 共 50 个只读端点
- 每个接口都补齐：
  - 产品语义（面向未接触过产品概念的读者）
  - 参数定义（类型、必填、默认值、范围、含义）
  - 枚举有效值及解释
  - 响应字段说明
  - 错误码与触发条件
  - `curl` 示例（host 固定 `http://127.0.0.1:35511`）
- 新增“对象结构字典（字段级）”章节：
  - 将文档中引用的 object（如 `chatStateView`、`monitorAgentOverviewItem`、`worldEvolutionSnapshot`、`store.KBProposal` 等）全部展开到具体字段。
- 对接口响应表做二次收敛：
  - 清理所有泛化 `object` 写法，改为明确类型（例如 `item/settings/status_count/unread_backlog/cost_recent`）。
  - 对 map 结构给出 value 结构定义（例如 `by_type -> costSummaryAgg`）。
  - 修正 `GET /api/v1/world/evolution-alerts` 中告警字段为 `alerts[].severity`（非 `alerts[].level`）。
- 在 `doc/change-history.md` 追加本次拆分记录。

## 为什么改

- 视觉展示开发者只需要“读数据”的接口文档，不需要写接口与管理动作。
- 原 dashboard 总文档同时包含读写接口，不利于新人快速理解世界观与对接边界。
- 通过单独只读文档，降低接入复杂度，避免误调写接口。

## 如何验证

- 结构校验：
  - `rg '^### \`GET ' doc/runtime-dashboard-readonly-api.md | wc -l` 得到 50
  - `rg '^### \`((POST|PUT|DELETE|PATCH))' doc/runtime-dashboard-readonly-api.md` 无结果
- 覆盖校验：
  - `awk` 检查每个 GET 章节均含参数说明段（通过）
  - `awk` 检查每个 GET 章节均含 `curl` 示例（通过）
  - 对象结构覆盖检查：接口引用的 object 在“对象结构字典”中均有字段定义（通过）
  - `rg` 检查文档中不存在泛化 `| object |` 或 `| array |` 行（通过）
- 基线回归：
  - `go test ./...` 通过

## 对 agents 的可见变化

- 无运行时行为变化（仅文档变更）。
- agent-facing 协议与接口实现未改动。
