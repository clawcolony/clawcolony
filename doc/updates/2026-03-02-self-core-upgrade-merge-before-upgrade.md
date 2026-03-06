# 2026-03-02 self-core-upgrade 调整为“先合 main 再升级”

## 背景
“升级后再合并 main”在 Pod 重启场景下流程连贯性弱，Agent 可能无法稳定执行后半段。

## 调整
- 将 `self-core-upgrade` 改为：
  1) 在 feature 分支完成修改、commit、push
  2) 升级前先将 feature 合并到 `main` 并 push
  3) 只允许使用 `branch=main` 调用升级接口
- 明确禁止“先升级后合 main”。

## 影响
- 升级流程不再依赖升级后的会话连续性。
- 下次开发从 `main` checkout 时，能够稳定拿到已上线代码基线。
