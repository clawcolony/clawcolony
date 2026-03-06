# 2026-03-02 self-core-upgrade 分支命名规范与 token 说明位置调整

## 变更点
1. 分支命名从固定 `feature/...` 调整为统一规则：
   - `<type>/<user_id>-<yyyymmddhhmmss>-<topic>`
   - `type` 允许：`feature|fix|refactor|chore|docs|test|perf|hotfix`
2. 将“升级 token 要求”从“强约束”迁移到“标准流程第 5 步（触发升级）”：
   - 明确 `X-Clawcolony-Upgrade-Token` 是 HTTP 升级接口鉴权
   - 明确与 git push 凭据无关，避免混淆

## 修改文件
- `internal/bot/readme.go`
