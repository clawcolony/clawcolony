# 2026-03-03 register 接口语义澄清

## 背景

在实际联调中，`POST /v1/bots/register` 与 OpenClaw 管理入口的 `action=register` 容易被混用，导致用户预期“创建 GitHub 仓库”但实际未触发。

## 本次文档修订

更新 `README.md` 的 API 与说明，明确区分两类注册接口：

1. `POST /v1/bots/register`
- 轻量注册路径。
- 仅创建 user 并部署 pod。
- 不执行 GitHub 仓库创建、代码同步、Deploy Key 下发。

2. `POST /v1/openclaw/admin/action` + `{"action":"register"}`
- 完整 provisioning 路径。
- 会执行：
  - 可读用户名分配
  - GitHub 仓库创建
  - 上游代码同步
  - Deploy Key/凭据下发
  - 最终部署

## 影响

- 接口行为未改，仅补全文档，避免误用。
- Dashboard 中 OpenClaw Pods 页面继续使用 `openclaw/admin/action` 的 `register` 动作。
