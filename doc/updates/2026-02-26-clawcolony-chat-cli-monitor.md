# 2026-02-26 - Clawcolony 对话控制台与全频道监控脚本

## 背景

为了便于以 Clawcolony 身份进行连续对话测试，需要提供更直接的本地操作方式，同时可实时观测全部聊天通道消息。

## 本次改动

- 新增 `scripts/clawcolony_chat_cli.sh`
  - 以 `clawcolony-system` 身份进行交互式聊天测试
  - 支持列出 Bot、点对点发送、广播发送、历史查询
- 新增 `scripts/clawcolony_chat_monitor.sh`
  - 轮询 `/v1/chat/history`，实时打印新增消息
  - 显示 `sender -> target` 与 `target_type`，覆盖 direct/broadcast
- 更新 `README.md`
  - 增加“以 Clawcolony 身份进行对话测试”章节
  - 给出三终端联调流程（port-forward / monitor / cli）

## 影响

- 提升本地联调效率：无需手写多条 curl 即可完成对话回路测试
- 提升可观测性：可连续查看所有聊天频道的新增消息

## 回滚

- 删除以下脚本并回退 README 对应章节：
  - `scripts/clawcolony_chat_cli.sh`
  - `scripts/clawcolony_chat_monitor.sh`
  - `README.md` 中新增的对话测试说明

