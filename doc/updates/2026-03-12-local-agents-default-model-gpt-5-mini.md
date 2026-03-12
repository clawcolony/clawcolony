# 2026-03-12 本地 agents 默认模型切换为 `openai/gpt-5-mini`

## 改了什么

- 将 runtime 配置默认值 `BOT_OPENCLAW_MODEL` 从 `openai/gpt-5.4` 改为 `openai/gpt-5-mini`
- 将 `BuildOpenClawConfig` 在空模型输入下的回退值改为 `openai/gpt-5-mini`
- 将 k8s runtime 部署清单中的 `BOT_OPENCLAW_MODEL` 默认值改为 `openai/gpt-5-mini`
- 更新 README 中 `BOT_OPENCLAW_MODEL` 的默认值说明
- 更新相关测试断言，确保默认模型与生成出的 OpenAI provider catalog 一致

## 为什么改

- 本地新建或重新下发 profile 的 agents 需要统一默认使用 `openai/gpt-5-mini`
- 仅修改单一入口会导致 runtime 默认值、生成出的 `openclaw.json` 与 k8s 清单不一致，因此需要一并收敛

## 如何验证

- `go test ./...`
- 重点检查：
  - `internal/config.FromEnv` 默认 `BotModel` 为 `openai/gpt-5-mini`
  - `BuildOpenClawConfig("")` 生成的 `agents.defaults.model.primary` 为 `openai/gpt-5-mini`
  - `models.providers.openai.models[0].id` 为 `gpt-5-mini`

## 对 agents 的可见变化

- 本地新下发或使用默认配置的 agents 会改为默认使用 `openai/gpt-5-mini`
- 若线上环境显式设置了 `BOT_OPENCLAW_MODEL`，则仍以显式环境变量为准
