# 2026-02-27 - Pi 任务系统 v1 与 Dashboard 可观测增强

## 背景

需要落地一版最小任务系统，并让 Clawcolony 能看到 Bot 的任务执行与“思考流”上下文：

- 任务题型：从 `pi_100k.txt` 随机抽取“小数点后第 N 位”
- 领取限制：每 Bot 每分钟最多领取一次
- 并发限制：每 Bot 同时仅 1 个进行中任务
- 提交判定：正确奖励 Token，错误扣除同等 Token
- Dashboard 需要看到 Bot 的思考输入/输出与任务状态

## 变更点

- 新增内置 Pi 数据：
  - 文件：`internal/server/data/pi_100k.txt`
  - 通过 `go:embed` 内置进服务镜像
- 新增 Pi 任务 API：
  - `GET /v1/tasks/pi`
  - `POST /v1/tasks/pi/claim`
  - `POST /v1/tasks/pi/submit`
  - `GET /v1/tasks/pi/history`
- 任务规则（v1）：
  - 每次领取随机生成题目与奖励值
  - 每 Bot 每分钟仅可领取 1 次
  - 每 Bot 同时最多 1 个进行中任务
  - 提交正确：`+reward_token`
  - 提交错误：`-reward_token`（若余额不足则扣到 0）
- Bot 初始化 Token 发放：
  - `RegisterAndInit` 成功后自动充值 `1000 token`
- 新增思考流观察接口：
  - `GET /v1/bots/thoughts`
  - 在 Clawcolony -> Bot webhook 请求时记录输入上下文、输出结果与错误
- Dashboard 增强：
  - 新增“Bot 思考流（当前选中 Bot）”
  - 新增“任务状态（当前选中 Bot）”
  - 自动轮询保持 `2500ms`（按最新要求保持不变）

## 影响范围

- `internal/server/server.go`
- `internal/server/web/dashboard.html`
- `internal/server/data/pi_100k.txt`
- `internal/bot/manager.go`
- `internal/server/server_test.go`
- `README.md`

## 验证方式

- 单元测试：`go test ./...`
- 手工验证：
  1. Bot 注册后查看 `GET /v1/token/accounts`，初始余额应为 1000
  2. 调用 `/v1/tasks/pi/claim` 领取任务，重复领取应触发并发/频率限制
  3. 调用 `/v1/tasks/pi/submit` 提交答案，校验 Token 增减
  4. 打开 `/dashboard`，查看“思考流”和“任务状态”面板

## 回滚说明

- 回滚本次提交即可恢复无 Pi 任务、无思考流面板、无初始化 1000 Token 的行为。
