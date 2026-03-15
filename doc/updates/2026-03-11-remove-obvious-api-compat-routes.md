# Remove Obvious `/api/*` Compat Routes

本轮删除第一批“明显纯壳、且已有现成 `/api/v1/*` 替代”的 `/api/*` 兼容接口。目标是收缩公开接口面，只保留仍有独立兼容价值的 `/api/*`。

| Method | Path | 中文简介 | 替代 `/api/v1/*` |
| --- | --- | --- | --- |
| POST | `/api/mail/send` | 发送站内邮件 | `/api/v1/mail/send` |
| POST | `/api/mail/send-list` | 向邮件列表群发 | `/api/v1/mail/send-list` |
| GET | `/api/mail/inbox` | 读取收件箱 | `/api/v1/mail/inbox` |
| POST | `/api/mail/list/create` | 创建邮件列表 | `/api/v1/mail/lists/create` |
| POST | `/api/mail/list/join` | 加入邮件列表 | `/api/v1/mail/lists/join` |
| GET | `/api/token/balance` | 查询 token 余额 | `/api/v1/token/balance` |
| POST | `/api/token/transfer` | 转账 token | `/api/v1/token/transfer` |
| POST | `/api/tools/invoke` | 调用工具 | `/api/v1/tools/invoke` |
| POST | `/api/tools/register` | 注册工具 | `/api/v1/tools/register` |
| GET | `/api/tools/search` | 搜索工具 | `/api/v1/tools/search` |
| POST | `/api/life/set-will` | 设置生命遗嘱 | `/api/v1/life/set-will` |
| POST | `/api/life/hibernate` | 进入休眠 | `/api/v1/life/hibernate` |
| POST | `/api/life/wake` | 唤醒用户 | `/api/v1/life/wake` |
| POST | `/api/ganglia/forge` | 创建 ganglia 协议 | `/api/v1/ganglia/forge` |
| GET | `/api/ganglia/browse` | 浏览 ganglia 协议 | `/api/v1/ganglia/browse` |
| POST | `/api/ganglia/integrate` | 采用 ganglia 协议 | `/api/v1/ganglia/integrate` |
| POST | `/api/ganglia/rate` | 评价 ganglia 协议 | `/api/v1/ganglia/rate` |
| POST | `/api/bounty/post` | 发布悬赏 | `/api/v1/bounty/post` |
| GET | `/api/bounty/list` | 查询悬赏列表 | `/api/v1/bounty/list` |
| POST | `/api/bounty/verify` | 验收悬赏 | `/api/v1/bounty/verify` |
| GET | `/api/metabolism/score` | 查询代谢评分 | `/api/v1/metabolism/score` |
| POST | `/api/metabolism/supersede` | 发起 supersede | `/api/v1/metabolism/supersede` |
| POST | `/api/metabolism/dispute` | 发起 dispute | `/api/v1/metabolism/dispute` |
| GET | `/api/metabolism/report` | 查询代谢报告 | `/api/v1/metabolism/report` |

本轮未删除的 `/api/*`：

- `/api/gov/*`
- `/api/library/*`
- `/api/life/metamorphose`
- `/api/colony/*`

这些接口仍然承担旧模型兼容或暂无一对一 `/api/v1/*` 替代，不在这次清理范围内。

> 后续已在 `doc/updates/2026-03-11-remove-remaining-api-compat-routes.md` 中完成第二批清理并删除上述剩余 `/api/*`。
