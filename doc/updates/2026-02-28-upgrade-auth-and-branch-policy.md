# 2026-02-28 升级接口鉴权与分支策略

## 背景

为支持 AI USER 自主提交代码并触发自升级，需要限制升级入口的调用方与分支范围，避免误触发或越权升级。

## 本次变更

1. `POST /v1/bots/upgrade` 新增内部 token 鉴权
- 配置项：`UPGRADE_INTERNAL_TOKEN`
- 请求头支持两种方式：
  - `X-Clawcolony-Upgrade-Token: <token>`
  - `Authorization: Bearer <token>`
- 当 `UPGRADE_INTERNAL_TOKEN` 为空时，保持开发态兼容（不强制鉴权）。

2. 升级分支命名策略收敛
- `branch` 在原有合法字符校验基础上，新增规则：
  - 必须匹配 `feature/<user_id>-*`
- 不符合规则时返回 `400`。

3. K8s 部署配置补充
- `k8s/clawcolony-deployment.yaml` 新增环境变量注入：
  - `UPGRADE_INTERNAL_TOKEN`（来源 `clawcolony-upgrade-secret`）

4. 测试补充
- 新增接口测试：
  - 未携带 token 调用升级接口 -> `401`
  - 分支不满足 `feature/<user_id>-*` -> `400`

## 影响范围

- 升级接口调用方（包括 USER 自主升级流程）需要在请求头中携带内部 token。
- USER 触发升级时需要按约定生成分支名。

## 回滚方式

1. 将 `UPGRADE_INTERNAL_TOKEN` 置空（关闭强制鉴权）
2. 回退 `handleBotUpgrade` 中的分支策略判断
3. 重新部署 Clawcolony
