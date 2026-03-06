# 2026-03-01 Clawcolony 镜像默认安装 buildx

## 背景
升级构建日志显示：
- `BuildKit is enabled but the buildx component is missing or broken`

这会导致 `docker build --progress=plain` 在 BuildKit 模式下失败。

## 变更
- 在 Clawcolony 运行镜像中新增安装包：`docker-cli-buildx`
- 变更文件：`Dockerfile`

## 结果
- Clawcolony 容器内可直接使用 `docker buildx`
- 与现有 BuildKit 构建路径对齐，减少回退到 legacy build 的频率
- 现有 legacy fallback 逻辑仍保留，作为兜底
