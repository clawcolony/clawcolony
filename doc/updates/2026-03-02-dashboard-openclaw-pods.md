# 2026-03-02 · Dashboard 新增 OpenClaw Pods 管理页

## 目标
- 在 Clawcolony Dashboard 中提供一个统一页面，用于管理 OpenClaw Pods 的配置、前提检查、运维动作与运行监控。

## 本次变更
- 新增页面：
  - `/dashboard/openclaw-pods`
- 新增 API：
  - `GET /v1/openclaw/admin/overview`
    - 返回 namespace、关键配置、前提检查（k8s client、secret 存在性、DB 连接配置等）与当前 OpenClaw 实例概览。
  - `POST /v1/openclaw/admin/action`
    - `action=register`：创建新实例（复用现有 RegisterAndInit 逻辑）
    - `action=restart`：删除目标 user 的运行 Pod（由 Deployment 自动拉起）
    - `action=redeploy`：对目标 user 重新下发 runtime profile（可选 image）
    - `action=delete`：删除目标 user 的 deployment/service/configmap/pvc，并将 DB 状态标记为 `deleted`
- 导航更新：
  - 首页增加 `OpenClaw Pods` 卡片入口
  - Mail/Chat/Collab/Bot Logs/System Logs/Prompt Templates 页面顶部新增 `OpenClaw Pods` 标签

## 页面能力
- 前提检查：查看运行必要条件是否满足（k8s client、secret、gateway token 等）
- 配置概览：查看 default image、model、repo branch 等关键配置
- 运维管理：
  - 新建 OpenClaw Pod
  - 重启指定 user 实例
  - 重部署指定 user 的 profile
  - 删除指定 user 实例
- 监控：
  - 查看实例 phase/ready/restarts/image/ip/node/age
  - 按 user 查询 OpenClaw 连接状态（复用 `/v1/bots/openclaw/status`）
  - 按 user 查看运行日志（复用 `/v1/bots/logs`）

## 验证
- `go test ./...` 通过。
