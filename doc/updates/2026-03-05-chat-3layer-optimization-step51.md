# 2026-03-05 Chat 三层优化（Step 51）

## 本步目标

把聊天慢的问题按“三层”落地：

1. 调度层：避免旧消息堆积拖慢最新对话（latest-wins + 可取消）。
2. 执行层：把超时/重试/并发参数化，支持快速调优。
3. 可观测层：Dashboard 直接展示聊天任务状态与失败原因。

## 代码变更

1. 后端队列与状态机（`internal/server/server.go`）

- 新增聊天任务调度器：
  - 每条 chat send 生成 `chat_task_id`
  - 支持 `queued/running/succeeded/failed/canceled/timeout`
  - `latest-wins` 时会淘汰旧 pending，并可取消当前 running
- 新增 worker 池执行模型（全局并发闸门 + 每 user 串行）
- 新增状态 API：
  - `GET /v1/chat/state?user_id=<id>`

2. 执行链路调优（`internal/config/config.go`, `internal/server/server.go`）

- 新增可配置项：
  - `CLAWCOLONY_CHAT_REPLY_TIMEOUT`
  - `CLAWCOLONY_CHAT_WORKERS`
  - `CLAWCOLONY_CHAT_QUEUE_SIZE`
  - `CLAWCOLONY_CHAT_EXEC_MAX_CONCURRENCY`
  - `CLAWCOLONY_CHAT_LATEST_WINS`
  - `CLAWCOLONY_CHAT_CANCEL_RUNNING`
  - `CLAWCOLONY_CHAT_WARMUP_RETRIES`
  - `CLAWCOLONY_CHAT_SESSION_RETRIES`
  - `CLAWCOLONY_CHAT_RETRY_DELAY`
- `sendChatToOpenClaw` 重试与等待改为配置驱动，并支持 context-aware sleep（可中断）。

3. Dashboard 可视化（`internal/server/web/dashboard_chat.html`）

- 新增 Chat pipeline 状态区，显示：
  - workers / queued users / backlog
  - running task / pending task
  - recent 成功、失败、取消、超时计数
  - last_error
- 发送消息后展示 task id，并主动刷新状态。

4. 测试（`internal/server/server_test.go`）

- 新增 `TestChatLatestWinsCancelsRunningAndExecutesNewest`
  - 验证 running 任务可被后续消息取消
  - 验证最新消息可成功执行并写入聊天历史

## 测试验证

```bash
go test ./internal/server -count=1
go test ./... -count=1
```

结果：

- 全部通过。

## 结论

聊天链路已从“无状态 goroutine + 固定超时”升级为“可调度、可取消、可观测”的三层模型，可显著降低高频对话下的堆积与不可见失败问题。
