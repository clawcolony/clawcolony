# 2026-03-09 dev_minikube 部署保留 `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL`

## 改了什么

- 调整 runtime deployment 清单：
  - `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL` 从硬编码空字符串改为模板占位符 `{{CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL}}`
- 调整 `scripts/dev_minikube.sh` 渲染逻辑：
  - 支持通过环境变量传入 `CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL`
  - 当环境变量未提供时，自动读取当前 `freewill/deploy/clawcolony-runtime` 已有值并复用
  - 应用 manifest 时同时替换 `{{CLAWCOLONY_IMAGE}}` 和 `{{CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL}}`
- 调整 `Makefile` 的 `deploy` 目标：
  - 同步替换 `{{CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL}}`，避免把占位符字面量写进 deployment
  - 新增 `PREVIEW_PUBLIC_BASE_URL` 参数（可选）

## 为什么改

- 之前 deployment 清单固定写 `value: ""`，每次 `dev_minikube.sh` 重新部署都会把线上已设置的 preview public base url 覆盖为空。
- 导致 `link_create` 响应里的 `public_url` 消失，agent 会回退到内网不可达 `absolute_url`。

## 如何验证

- 先手动设置一次：
  - `kubectl -n freewill set env deploy/clawcolony-runtime CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL=http://127.0.0.1:35511`
- 再执行 `./scripts/dev_minikube.sh <image>`
- 可选验证 `make deploy`：
  - `make deploy IMAGE=<image> PREVIEW_PUBLIC_BASE_URL=http://127.0.0.1:35511`
- 验证部署后变量仍在：
  - `kubectl -n freewill get deploy clawcolony-runtime -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL")].value}'`
- 调用 `/api/v1/bots/dev/link`，确认响应继续包含 `item.public_url`

## 对 agents 的可见变化

- 重新部署 runtime 后，`public_url` 配置不再被意外清空；agent 返回可直接打开链接的稳定性提升。
