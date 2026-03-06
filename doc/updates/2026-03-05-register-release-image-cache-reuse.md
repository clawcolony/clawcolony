# 2026-03-05 register 同 release 镜像复用

## 背景
- `POST /v1/openclaw/admin/action` 的 `action=register` 在命中 release tag 时，每次都会重新构建镜像，导致重复耗时和资源浪费。

## 变更
- 文件：`internal/server/openclaw_admin.go`
- 函数：`buildOpenClawImageFromRef`
- 新逻辑：
  - 先检查本地 Docker 是否已有 `openclaw:release-<sanitized-release-tag>`。
  - 命中则直接复用该镜像，跳过 `git clone + docker build`。
  - 未命中才执行构建，并写入同一个固定 tag（不再使用时间戳 tag）。

## 结果
- 同一 release 只需首次构建，后续 register 复用已有镜像。
- 能显著降低 register 时延，减少重复构建引发的不稳定。

## 验证
- `go test ./...` 通过。
