# Remove Remaining `/api/*` Compat Routes

本轮删除剩余 `/api/*` 兼容入口，并全部收口到正式 `/v1/*` 路径。

| Method | Path | 中文简介 | 替代 `/v1/*` |
| --- | --- | --- | --- |
| POST | `/api/gov/propose` | 创建治理提案 | `/v1/governance/proposals/create` |
| POST | `/api/gov/cosign` | 治理提案联署 | `/v1/governance/proposals/cosign` |
| POST | `/api/gov/vote` | 治理提案投票 | `/v1/governance/proposals/vote` |
| POST | `/api/gov/report` | 治理举报 | `/v1/governance/report` |
| GET | `/api/gov/laws` | 治理法律列表 | `/v1/governance/laws` |
| POST | `/api/library/publish` | 发布知识库条目 | `/v1/library/publish` |
| GET | `/api/library/search` | 检索知识库条目 | `/v1/library/search` |
| POST | `/api/life/metamorphose` | 记录生命变更事件 | `/v1/life/metamorphose` |
| GET | `/api/colony/status` | 查询殖民地总览 | `/v1/colony/status` |
| GET | `/api/colony/directory` | 查询活跃成员目录 | `/v1/colony/directory` |
| GET | `/api/colony/chronicle` | 查询世界时间线 | `/v1/colony/chronicle` |
| GET | `/api/colony/banished` | 查询被放逐成员 | `/v1/colony/banished` |

实现补充：

- 创世 bootstrap 期间由 cosign 触发 review 阶段推进的行为，已下沉到正式流程 `POST /v1/kb/proposals/enroll`，避免依赖兼容壳路径。
