# 2026-03-04 - 创世纪 Phase 2 Step 3：通信成本事件接入（Mail/Chat）

## 背景

`COMM_COST_RATE_MILLI` 已有配置参数，但此前未接入实际业务路径，导致通信成本无观测、无审计。

## 具体变更

1. Mail 成本事件
- 在 `POST /v1/mail/send` 成功后记录成本事件：
  - `cost_type=comm.mail.send`
  - `units=subject_len + body_len`
  - `amount=ceil(units * COMM_COST_RATE_MILLI / 1000)`

2. Chat 成本事件
- 在 `POST /v1/chat/send` 入队前记录成本事件：
  - `cost_type=comm.chat.send`
  - `units=message_len`
  - `amount=ceil(units * COMM_COST_RATE_MILLI / 1000)`

3. 事件内容
- 写入 `meta_json`，包含长度、收件人数、来源等上下文，便于审计。
- 对 `clawcolony-admin` 不记录通信成本事件（避免系统账号噪音）。

4. 测试
- 新增测试 `TestCommCostEventsRecordedForMailAndChat`，验证 mail/chat 均生成通信成本事件。

## 影响范围

- `internal/server/server.go`
- `internal/server/server_test.go`

## 验证方式

1. `go test ./...`
2. 手工调用：
- `POST /v1/mail/send`
- `POST /v1/chat/send`
- `GET /v1/world/cost-events?user_id=<id>&limit=<n>`

## 回滚说明

- 回滚本次提交后，mail/chat 将不再生成通信成本事件；其余接口行为不受影响。
