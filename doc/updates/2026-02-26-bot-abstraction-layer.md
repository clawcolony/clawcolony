# 2026-02-26 AI Bot 抽象层落地

## 基本信息

- 日期：2026-02-26
- 变更主题：统一 AI Bot 抽象层（命名、ID、初始化记录）
- 责任人（人类 / AI Bot）：AI Bot（Codex）
- 关联任务/PR：N/A（本地开发）

## 变更背景

项目后续要接入不同类型的 AI Bot（如 OpenClaw），需要将 Bot 的注册与初始化流程从具体实现中解耦，避免业务逻辑直接耦合到某个 provider。

## 具体变更

- 新增 `internal/bot` 抽象层：
  - `Manager`：统一处理 Bot 注册、ID 分配、名字生成、初始化状态
  - `DeploySpec`：统一部署入参模型
  - `Deployer` 接口：为不同 Bot provider 预留实现扩展点
  - `NoopDeployer`：当前默认实现，用于开发与测试链路
- 扩展 `store` Bot 模型字段：
  - `name`、`provider`、`status`、`initialized`、`updated_at`
- 新增 `Store.UpsertBot`，统一 Bot 记录创建/更新语义
- 新增 API：`POST /v1/bots/register`
  - 触发 Bot 注册并返回初始化后的记录
- 保持 `GET /v1/bots` 可直接查看 Bot 记录列表
- 新增测试覆盖 Bot 注册与列表行为

## 影响范围

- 影响模块：Bot 管理、存储模型、服务 API、测试
- 影响 namespace：逻辑层，无新增 namespace
- 是否影响兼容性：新增接口与字段，不破坏现有接口路径

## 验证方式

- `go test ./...`
- `make check-doc`
- 验证接口：
  - `POST /v1/bots/register`
  - `GET /v1/bots`

## 回滚方案

- 回滚到上一提交，恢复原 Bot 简化模型

## 备注

该抽象层是后续接入 OpenClaw/Kubernetes 实际部署器的基础，当前默认部署器为 `Noop`。
