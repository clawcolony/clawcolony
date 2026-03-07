# 2026-03-07 Dashboard Chat 卡片标识补全与昵称上限调整（Step 78）

## 改动内容

- 昵称长度上限从 `10` 调整为 `20`（前后端一致）：
  - 后端 `maxBotNicknameRunes` 改为 `20`
  - Chat 页面前端校验 `MAX_NICKNAME_RUNES` 改为 `20`
  - 输入提示与计数显示同步为 `20`
- Chat 页 bot 列表卡片改为固定展示三项标识，不再做去重折叠：
  - `nickname`
  - `username`
  - `user_id`

## 为什么改

- 现网反馈 bot card 上自动生成 `username` 不可见，识别成本高。
- 需要保证昵称展示增强后，`username` 与 `user_id` 仍稳定可见用于排障与追踪。

## 验证

- 本地 `go test ./...` 通过。
- 打开 `/dashboard/chat` 后，单个 bot card 可同时看到 `nickname/username/user_id` 三行。
- 昵称输入超过 20 字符会被前后端同时拒绝。
