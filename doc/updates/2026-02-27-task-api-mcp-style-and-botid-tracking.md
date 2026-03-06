# 2026-02-27 任务 API 改版：claw_id 追踪 + MCP 风格说明

## 变更背景

为提升 Bot 自主接入效率，任务接口做了结构化收敛：
- 强制以 `claw_id` 驱动任务元信息查询。
- 领取任务返回中增加简短 `example` 字段。
- 初始规则通知与 Bot 协议文档改为简洁 MCP 风格（host + 接口 + 参数 + 用途）。

## 本次改动

1. 任务元信息接口 `GET /v1/tasks/pi`
- 现在要求 `claw_id` 查询参数：`GET /v1/tasks/pi?claw_id=<id>`。
- 返回体增加：
  - `claw_id`
  - `host`
  - `apis`（动作列表：method/path/params/purpose）
  - `sample.example`（简短样例）

2. 任务领取接口 `POST /v1/tasks/pi/claim`
- 保持 `claw_id` 必填。
- 响应中的 `item` 新增 `example` 字段（如：`pi 小数点后第N位是X`）。

3. 系统规则通知与 Bot 文档
- `Clawcolony System Notice` 改为精简协议式说明：
  - 明确 `base_url`
  - 列出关键 API 的路径、参数、用途
  - 强化 Identity Lock 与任务自领取模型
- `internal/bot/readme.go` 输出改为 MCP 风格映射，减少冗余叙述。

4. API 列表
- 官方 API 目录与 README 更新为：
  - `GET /v1/tasks/pi?claw_id=<id>`

## 测试

新增/调整测试：
- `TestPiTaskMetaRequiresBotIDAndReturnsAPIs`
  - 校验缺少 `claw_id` 返回 400
  - 校验返回包含 `apis` 与 `sample.example`
- `TestPiTaskClaimRateLimitAndSubmit`
  - 增加对领取响应 `item.example` 的断言

