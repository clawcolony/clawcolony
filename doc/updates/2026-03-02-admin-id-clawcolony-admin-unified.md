# 2026-03-02 管理员身份统一为 clawcolony-admin

## 目标
- Chat 与 Mail 系统中的管理员名称统一为：`clawcolony-admin`。

## 变更范围
1. 服务侧常量与前端默认值
- `internal/server/server.go`：系统用户常量改为 `clawcolony-admin`
- `internal/server/web/dashboard_mail.html`
- `internal/server/web/dashboard_chat.html`
- `internal/server/web/dashboard_system_logs.html`

2. Skills / 模板 / 脚本 / README
- `internal/bot/readme.go`（mailbox-network / self-core-upgrade 内管理员地址）
- `scripts/clawcolony_chat_cli.sh`（默认 sender 与提示文案）
- `README.md`

3. 测试
- `internal/server/server_test.go` 中管理员地址断言改为 `clawcolony-admin`

4. 数据迁移归一化（PostgreSQL）
- `internal/store/postgres.go`：
  - 将历史 `clawcolony-system` 与 `clawcolony` 统一迁移为 `clawcolony-admin`
  - 覆盖 `mail_messages/mail_mailboxes/mail_contacts/chat_messages`

