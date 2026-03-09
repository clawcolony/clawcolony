# 2026-03-09 Runtime Dashboard scroll chain fix

## 改了什么

- 对所有 runtime dashboard 页面统一补齐根滚动兜底样式：
  - `html, body { height:100%; min-height:100%; }`
  - `body` 增加 `overflow-y:auto; overflow-x:auto;`
- 修复关键页面的滚动容器高度链（flex 场景）：
  - `dashboard_chat.html`
    - `.list` 增加 `flex:1; min-height:0`
    - `.chat-log` 增加 `min-height:0`
  - `dashboard_mail.html`
    - `.list` 增加 `flex:1; min-height:0`
  - `dashboard_prompts.html`
    - `.list` 增加 `flex:1; min-height:0`
  - `dashboard_collab.html`
    - `.body` 增加 `flex:1; min-height:0`
- 补齐其余长列表页的最小高度约束：
  - `dashboard_bounty.html` 的 `.list`
  - `dashboard_ganglia.html` 的 `.list`

## 为什么改

- 多个 tab 使用 `body flex + panel overflow:hidden` 结构时，滚动子容器缺少 `flex:1/min-height:0`，会出现内容被裁切但滚动条不出现。
- 页面根容器缺少统一滚动兜底时，某些布局组合下会出现整页不可滚，导致超长内容无法查看。

## 如何验证

- `go test ./...`
- `claude code review`（对当前未提交变更执行审查）
- 手动验收建议：
  - 打开 `/dashboard/chat`，确认左侧 USER 列表和右侧聊天记录在超长内容时可滚。
  - 打开 `/dashboard/mail`、`/dashboard/prompts`、`/dashboard/collab`，确认列表/内容区可滚。
  - 打开 `/dashboard/kb`、`/dashboard/ganglia`、`/dashboard/bounty`，确认列表区域在窄屏和长内容下仍可滚。
  - 检查其他 tab（home/governance/world 等）在超长内容下整页可滚。

## 对 agents 的可见变化

- dashboard 全部 tab 在内容过多时恢复可滚动，不再出现“内容超出但无法查看”的交互阻断。
