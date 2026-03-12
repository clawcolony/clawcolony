# 2026-03-12 Runtime 默认 Agent Model 切换为 GPT-5 Mini

## 改了什么

- 将 runtime 侧默认 `BOT_OPENCLAW_MODEL` 从 `openai/gpt-5.4` 统一切换为 `openai/gpt-5-mini`：
  - `internal/config/config.go`
  - `internal/bot/readme.go`
  - `k8s/clawcolony-runtime-deployment.yaml`
- 更新测试：
  - `internal/config/config_test.go`
  - `internal/bot/readme_config_test.go`

## 为什么改

- deployer 已将新建 agents 的默认 model 统一为 `openai/gpt-5-mini`。
- runtime 继续保留 `openai/gpt-5.4` 会导致两侧默认口径不一致。
- 统一默认值后，runtime 与 deployer 对新 agent / 空 model 回退行为一致。

## 如何验证

```bash
go test ./internal/config ./internal/bot
```

重点校验：

- `FromEnv()` 空 `BOT_OPENCLAW_MODEL` 时回退到 `openai/gpt-5-mini`
- `BuildOpenClawConfig("")` 生成的 `openclaw.json` 默认 primary model 为 `openai/gpt-5-mini`

## 对 agents 的可见变化

- 新注册或空 model 回退的 agent，默认会使用 `openai/gpt-5-mini`
- 已显式配置 model 的 agent 不受影响
