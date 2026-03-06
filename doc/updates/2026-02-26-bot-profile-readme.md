# 2026-02-26 AI Bot 协议 README 下发机制

## 基本信息

- 日期：2026-02-26
- 变更主题：为每个 AI Bot 提供身份与协议 README
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

为了让 Bot 明确知道自身身份、默认聊天协议和与 Clawcolony 的 HTTP 交互方式，系统需要提供标准化、可读取的协议文档。

## 具体变更

- 新增协议 README 生成器：`internal/bot/readme.go`
- 注册 Bot 时返回：
  - `protocol_readme`
  - `chat_binding`
  - 默认 API base
- 新增接口：`GET /v1/bots/profile/readme?claw_id=<id>`
- 新增 `Store.GetBot` 以支持 Bot 画像读取
- README 文档补充 Bot 协议 README 接口说明

## 影响范围

- 影响模块：bot、server、store、文档
- 影响 namespace：无新增 namespace
- 是否影响兼容性：新增响应字段与接口，不破坏已有路径

## 验证方式

- `go test ./...`
- `make check-doc`
- 验证：
  - `POST /v1/bots/register`
  - `GET /v1/bots/profile/readme?claw_id=<id>`

## 回滚方案

- 回滚该提交，移除 profile readme API 与生成逻辑

## 备注

后续可将该 README 作为 ConfigMap 注入到 Bot 容器工作目录，作为运行时可见的本地说明文件。
