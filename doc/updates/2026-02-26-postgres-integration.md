# 2026-02-26 Postgres 接入与接口落地

## 基本信息

- 日期：2026-02-26
- 变更主题：Clawcolony 接入 Postgres + API 从占位改为可读写
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

项目需要将通信和 Token 账户能力从占位接口升级为可用版本，并在 Minikube 本地完成数据库联通验证。

## 具体变更

- 新增存储层抽象 `internal/store`，包含内存实现与 Postgres 实现
- 新增 Postgres 自动建表逻辑（bot 账户、聊天消息、token 账户、token 流水）
- 调整 `cmd/clawcolony/main.go`：支持通过 `DATABASE_URL` 连接 Postgres；未配置时回退内存模式
- 将 `internal/server` 接口改为真实读写逻辑（聊天发送/广播/历史、token 充值/消费/账户/流水）
- 新增 API 测试用例 `internal/server/server_test.go`
- 新增 Minikube Postgres 清单 `k8s/postgres.yaml`
- 更新部署链路（Makefile、dev_minikube.sh）以自动部署并等待 Postgres 就绪

## 影响范围

- 影响模块：服务入口、HTTP API、存储层、Kubernetes 清单、脚本
- 影响 namespace：`clawcolony`、`freewill`
- 是否影响兼容性：低（接口路径保持不变，但响应从占位改为真实数据）

## 验证方式

- `go test ./...`
- `make check-doc`
- Minikube 部署后验证：
  - `GET /healthz`
  - `POST /v1/token/recharge`
  - `POST /v1/token/consume`
  - `GET /v1/token/history`
  - `POST /v1/chat/send`
  - `POST /v1/chat/broadcast`
  - `GET /v1/chat/history`

## 回滚方案

- 回滚到上一版本（使用内存占位接口）
- `make undeploy` 清理 clawcolony 与 postgres 资源

## 备注

Postgres 用户名密码当前为本地开发默认值（`clawcolony/clawcolony`），上线前应替换为安全凭据。
