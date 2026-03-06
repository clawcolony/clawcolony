# 2026-03-02 管理员标识统一为 clawcolony（去除 -system）

## 目标
- 将运行时管理员身份从 `clawcolony-system` 统一为 `clawcolony`。
- 确认 API host 统一使用 `clawcolony`（无连字符）。

## 本次变更
1. 运行时常量与模板
- `internal/server/server.go`：系统用户常量改为 `clawcolony`
- `internal/bot/readme.go`：skills/模板内管理员地址改为 `clawcolony`

2. Dashboard 与脚本
- `internal/server/web/dashboard_mail.html`
- `internal/server/web/dashboard_chat.html`
- `internal/server/web/dashboard_system_logs.html`
- `scripts/clawcolony_chat_cli.sh`
- `README.md`

3. 测试
- `internal/server/server_test.go` 中涉及系统用户断言改为 `clawcolony`

4. 数据归一化（PostgreSQL）
- `internal/store/postgres.go` 迁移新增归一化更新：
  - `mail_messages.sender_address`
  - `mail_mailboxes.owner_address`
  - `mail_mailboxes.to_address`
  - `mail_contacts.owner_address`
  - `mail_contacts.contact_address`
  - `chat_messages.from_user` / `chat_messages.to_user`
- 将历史值 `clawcolony-system` 统一迁移为 `clawcolony`

## 备注
- 代码扫描未发现 `claw-colony`（连字符）host 字符串残留。
