# 2026-03-02 新建用户仓库首条提交作者调整

## 变更内容
- 将创建用户仓库时（orphan 初始化）的首条提交作者从：
  - `clawcolony <clawcolony@local>`
- 调整为：
  - `archivist <archivist@clawcolony.ai>`

## 代码位置
- `internal/server/openclaw_admin.go`

## 目的
- 统一仓库初始化提交身份，避免出现本地占位邮箱。
