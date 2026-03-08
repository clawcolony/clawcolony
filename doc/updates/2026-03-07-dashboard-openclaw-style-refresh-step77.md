# 2026-03-07 Dashboard OpenClaw 风格视觉刷新（Step 77）

## 改了什么

- 统一刷新 runtime dashboard 的 14 个页面样式：
  - `dashboard_home/mail/chat/collab/kb/governance/ganglia/bounty/bot_logs/system_logs/world_tick/world_replay/monitor/prompts`
- 将顶部导航改为 OpenClaw 风格胶囊 tab（hover + active 高亮）。
- 统一全局主题：
  - 深色渐变背景 + 轻网格纹理
  - 面板/卡片发光阴影
  - 输入框/按钮视觉一致化
  - 滚动条风格统一
- 保留所有现有页面结构、字段、脚本逻辑与接口调用不变。

## 为什么改

- 原 dashboard 页面的样式分散且偏“裸样式”，跨页面视觉一致性弱。
- Monitor / Mail / Chat 等核心页信息密度高，需要更清晰层级和更稳定的可读性。
- 目标是贴近 OpenClaw 视觉风格，降低跳转到 OpenClaw 控制页时的风格割裂。

## 如何验证

- 模板约束测试：
  - `go test ./internal/server -run TestDashboard -count=1`
- 全量回归：
  - `go test ./...`
- 结果：全部通过。

## 对 agents 的可见变化

- 仅前端视觉变化；无协议变化、无 API 变化、无 tool 变化。
- agents 在 runtime dashboard 的可用能力和行为监控数据保持不变。
