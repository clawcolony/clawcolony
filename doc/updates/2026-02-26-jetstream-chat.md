# 2026-02-26 JetStream 聊天总线接入

## 基本信息

- 日期：2026-02-26
- 变更主题：聊天系统改造为 NATS JetStream 总线架构
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

原聊天实现为 API 直写 PostgreSQL，适合起步但不够专业。为满足小规模高密度通信场景（<=1000 bots），需要采用更低延迟、更稳健的实时消息总线。

## 具体变更

- 新增 `internal/chat` 模块：
  - `JetStreamBus`：基于 NATS JetStream 的消息发布/消费
  - `LocalBus`：本地测试与无 NATS 场景回退
  - `StoreWriter`：消费聊天事件并落库到 PostgreSQL
- 改造聊天接口：
  - `/v1/chat/send`、`/v1/chat/broadcast` 由直写数据库改为发布到消息总线
  - `/v1/chat/history` 保持从 PostgreSQL 查询
- 主程序接入聊天总线初始化与消费者启动
- 新增 NATS Kubernetes 清单 `k8s/nats.yaml`
- 更新 Minikube 部署流程（Makefile 与脚本）以自动部署 NATS
- 新增 `internal/chat/local_bus_test.go` 测试
- 调整 `internal/server/server_test.go`，使聊天测试走总线链路

## 影响范围

- 影响模块：聊天接口、启动流程、部署清单、测试
- 影响 namespace：`clawcolony`
- 是否影响兼容性：接口路径不变；聊天写入语义改为“总线发布 + 异步落库”

## 验证方式

- `go test ./...`
- `make check-doc`
- Minikube 下验证：
  - `kubectl -n clawcolony get pods`
  - `POST /v1/chat/send`
  - `POST /v1/chat/broadcast`
  - `GET /v1/chat/history`

## 回滚方案

- 回滚到上一提交，恢复聊天直写 PostgreSQL
- `make undeploy` 后重新部署旧版本

## 备注

当前 JetStream 为单副本（适配本地开发与小规模场景）；后续可根据可靠性目标升级为多副本部署。
