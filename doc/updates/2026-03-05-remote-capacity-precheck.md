# 2026-03-05 远端容量前置检查（避免中途失败）

## 背景
远端“反复失败”的高频根因之一是：minikube 规格不足（尤其是默认 `2C/8G/20G`），导致以下后果在中后期才暴露：

- agent OOM / readiness 失败
- 镜像加载/构建阶段耗尽磁盘
- rollout 长时间等待后失败

## 改动

文件：`scripts/deploy_remote_stable.sh`

新增“前置容量检查”：

- 优先从 `minikube profile list -o json` 读取内存/CPU（docker 驱动下 CPU 配额字段常为 0）
- `docker inspect` 作为兜底来源
- 从 `minikube ssh` 读取 `/var` 可用磁盘
- 默认门槛：
  - `memory >= 24576 MiB`
  - `cpu >= 4`
  - `/var free >= 30 GiB`

若不满足，脚本直接失败并输出修复命令，不再进入后续部署阶段。

新增参数：

- `--minikube-min-memory-mb`
- `--minikube-min-cpus`
- `--minikube-min-disk-gb`

对应环境变量：

- `MINIKUBE_MIN_MEMORY_MB`
- `MINIKUBE_MIN_CPUS`
- `MINIKUBE_MIN_DISK_GB`

## 预期收益

- 将“中途失败”前移为“启动前失败”，节省排障与等待时间。
- 让远端环境不再以低规格误启动。
- 避免 CPU 误判为 0 导致的“假失败”。
