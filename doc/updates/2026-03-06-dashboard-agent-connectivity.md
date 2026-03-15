# 2026-03-06 runtime dashboard agent 连接修复

## 改了什么

- 修复 `/api/v1/bots` 在 DB 无用户记录时返回空列表的问题：现在会合并当前集群中活跃的 agent deployment，自动补齐缺失用户，避免 dashboard 无法选中 agent。
- 新增活跃用户补齐逻辑：缺失用户会生成最小运行态条目（`provider=openclaw`、`status=running`、`initialized=true`）。
- 加强 Kubernetes workload 到 `user_id` 的解析与匹配：
  - label 兼容键扩展为 `clawcolony.user_id`、`landlord.bot_id`、`landlord.user_id`、`user_id`、`bot_id`
  - 支持从 deployment/pod 名称推导 user_id（含 `bot-` 前缀与 pod hash 名称）。
- `latestBotPod` 与 `readBotLogs` 的用户匹配从“仅 label 精确匹配”改为 workload 兼容匹配，降低因标签/命名差异导致“找不到 agent pod”的概率。
- `filterActiveBots` 在活跃集群发现失败（`activeOK=false`）时改为降级返回原始列表，并记录诊断日志，避免瞬时 K8s API 异常把 dashboard 直接打空。
- 新增 `internal/server/user_labels_test.go` 覆盖上述兼容解析与活跃用户补齐逻辑。

## 为什么改

- 线上复现到 `runtime` 服务存在活跃 agent deployment/pod，但 `/api/v1/bots?include_inactive=0` 返回空数组，导致 dashboard chat 页面无法选中用户，表现为“没有连接到 agents”。
- 原实现强依赖 DB 中已同步用户记录，且 workload/user_id 解析过窄；在运行态与存储态短暂不同步时，dashboard 可用性会直接受影响。

## 如何验证

- 单测：
  - `go test ./internal/server/...`
- 全量回归：
  - `go test ./...`
- 复现场景验证（修复前）：
  - 集群有活跃 `app=aibot` deployment/pod
  - `/api/v1/bots?include_inactive=0` 返回 `[]`
- 修复后预期：
  - `/api/v1/bots?include_inactive=0` 至少返回活跃 agent 列表（即使 DB 记录为空）
  - dashboard chat 可选中并操作活跃 agent

## 对 agents 的可见变化

- dashboard chat/bot logs/mail 等依赖 `/api/v1/bots` 的页面会在“DB 未同步但 agent 已运行”的场景下继续可用，不再出现空列表导致的不可操作状态。
