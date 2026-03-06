# 2026-03-05 Dashboard 自发现第三轮：OpenClaw Pods / Prompts / Knowledge Base 一致性与移动端修复（Step 69）

## 目标
- 补齐三页交互一致性；
- 降低自动刷新对人工操作的干扰；
- 修复移动端下的可用性问题；
- 给关键行为加模板测试守卫，防止回退。

## 变更摘要

### 1) OpenClaw Pods 页
- 文件：`internal/server/web/dashboard_openclaw_pods.html`
- 新增：
  - 顶栏 `自动刷新` 开关（默认开启）
  - 定时刷新前先检查开关状态，允许人工排查时暂停自动轮询

### 2) Prompt Templates 页
- 文件：`internal/server/web/dashboard_prompts.html`
- 新增：
  - 顶栏 `自动刷新` 开关（默认开启）
  - 周期刷新策略：
    - 每 10s 刷新活跃 user 列表（分批轮询）
    - 编辑器未脏改时，周期刷新模板列表
  - `editorDirty` 脏状态保护：
    - 手动编辑期间不自动覆盖 textarea 内容
  - 移动端优化：
    - 小屏下卡片最小高度、列表最大高度、textarea 最小高度调整

### 3) Knowledge Base 页
- 文件：`internal/server/web/dashboard_kb.html`
- 新增：
  - 顶栏 `自动刷新` 开关（默认开启）
  - `user_id` 相关输入统一改为活跃 user 下拉：
    - 快速操作：`uid`
    - 创建提案：`c_uid`
  - 活跃 user 周期刷新并保持选择
  - Proposal/Detail 的自动刷新节拍统一到同一时钟（避免多定时器割裂）

## 测试与守卫
- 文件：`internal/server/dashboard_templates_test.go`
- 新增测试：
  - `TestDashboardPromptsKBPodsInteractionConsistency`
- 检查项：
  - 三页存在自动刷新开关与门控逻辑
  - KB 页已使用下拉而非自由文本 `user_id`
  - 禁止旧输入模式回退（`<input id="uid">` / `<input id="c_uid">`）

## 本地验证
- `go test ./internal/server -run "DashboardTopTabsConsistent|DashboardNoStaleUserListRefreshGuard|DashboardPromptsKBPodsInteractionConsistency"`
- 结果：通过
