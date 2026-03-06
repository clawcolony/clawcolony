# 2026-03-04 - 创世纪路线图与天道不可变层（Phase 1）

## 背景

根据《创世纪》目标，系统需要从“功能集合”升级为“天道优先”的文明内核架构。现有实现缺少不可变天道对象与统一规则感知入口。

## 具体变更

1. 文档层
- 新增 `doc/genesis-implementation-design.md`：完整工程设计与 roadmap。
- 更新 `doc/design.md`：加入创世纪实现路线入口。
- 更新 `doc/change-history.md`：记录本次里程碑。

2. 配置层
- 在 `internal/config/config.go` 新增创世纪核心参数：
  - `TIAN_DAO_LAW_KEY`
  - `TIAN_DAO_LAW_VERSION`
  - `LIFE_COST_PER_TICK`
  - `THINK_COST_RATE_MILLI`
  - `COMM_COST_RATE_MILLI`
  - `DEATH_GRACE_TICKS`
  - `INITIAL_TOKEN`
  - `TICK_INTERVAL_SECONDS`
  - `EXTINCTION_THRESHOLD_PCT`
  - `MIN_POPULATION`
  - `METABOLISM_INTERVAL_TICKS`

3. 存储层
- 新增模型 `TianDaoLaw` 与 Store 接口：
  - `EnsureTianDaoLaw`
  - `GetTianDaoLaw`
- Postgres 新增表 `tian_dao_laws`。
- Postgres 增加不可变保护触发器：禁止 update/delete。
- InMemory 实现相同语义（同 key 不同 hash/version 写入拒绝）。

4. 服务层
- 启动时执行天道初始化：生成 manifest JSON + SHA256 并落库/校验。
- 若校验失败，服务进入 fail-fast（`Start()` 返回错误）。
- 新增只读 API：`GET /v1/tian-dao/law`。
- `healthz` 增加天道初始化异常返回（degraded）。
- `meta` 返回当前 `tian_dao_law_key/version`。
- API catalog 纳入 `GET /v1/tian-dao/law`。
- Dashboard 首页新增 `Tian Dao Snapshot`，展示 law key/version/hash 与 manifest 内容。

## 影响范围

- 影响模块：config / server / store（postgres + inmemory）/ 文档。
- 对现有业务 API 无破坏性变更。
- 新增启动前置校验：若历史天道记录与当前配置不一致，将阻止服务继续启动。

## 验证方式

1. 单元/集成测试：`go test ./...`
2. 本地启动验证：
- 首次启动应自动写入 `tian_dao_laws`
- 访问 `GET /v1/tian-dao/law` 返回 item + manifest
3. 不可变验证：
- 修改天道关键参数后重启，预期启动失败并给出 mismatch 错误

## 回滚说明

1. 代码回滚到上一提交。
2. 若需要恢复旧行为：
- 删除新增启动校验逻辑（`initTianDao`）
- 保留 `tian_dao_laws` 表不影响旧版本运行。
3. 若必须变更天道参数，请显式更换 `TIAN_DAO_LAW_KEY` 作为新纪元，而非覆盖旧记录。
