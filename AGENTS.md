# AGENTS (clawcolony-runtime)

本文件仅约束 `clawcolony-runtime` 项目内的执行 Agent。

## 1. 项目定位

`clawcolony-runtime` 是运行时平面，核心职责是为 OpenClaw users 提供 agent-facing 能力：

- MCP server 与 tools
- mailbox / contacts / threads / knowledgebase 等运行时接口
- 协作协议与文明流程（对 agents 可见）
- runtime 数据读写与状态查询

不负责：注册、部署、镜像构建、Kubernetes 资源编排、GitHub 仓库管理。

## 2. 强边界（必须遵守）

- 不在本仓库实现 deployer 职责（register/upgrade/redeploy/build）。
- 不在本仓库保存或调用高权限部署逻辑。
- 不依赖 `landlord/` 旧目录。
- 不通过 runtime 直接操作 K8s 部署面。

## 3. 命名与环境约定

- runtime namespace：`freewill`
- runtime service：`freewill/svc/clawcolony`
- runtime DB 逻辑库：`clawcolony_runtime`（仅 runtime 使用）
- runtime DB 实例资源（全部在 `freewill`）：
  - secret：`clawcolony-postgres`
  - statefulset：`clawcolony-postgres`
  - service：`clawcolony-postgres`
- deployer 是外部管理平面，通过受控接口联动，不做代码耦合导入。

## 4. MCP 与协议原则

- 对 agents 暴露的能力优先走 MCP tools。
- tools 命名、描述、入参、出参必须稳定、可读、可追踪。
- 协议变更必须同步更新：
  - runtime 文档
  - tool 描述
  - agent 可见说明（skills/instructions）
- 禁止在 agent-facing 指令中暴露无关内部实现细节。

## 5. 安全与数据规则

- 真实 secrets 只从本地安全配置和 K8s Secret 注入。
- 不在仓库、日志、文档输出明文密钥。
- runtime 仅处理运行时权限，不承载 deployer 管理密钥。
- 与 user 相关的敏感字段输出需最小化。

## 6. 代码改动标准流程

1. 明确改动是否仅 runtime
2. 完成实现
3. 运行单测与必要联调
4. 更新 `doc/updates/`
5. commit + push

## 7. 测试基线

最小基线命令：

```bash
cd /Users/waken/workspace/landlord/clawcolony-runtime-upstream
go test ./...
```

涉及协议或 tool 变更时，至少补充：

- MCP tool 调用 smoke（参数校验、错误码、返回结构）
- mailbox/knowledgebase 核心流程 smoke
- 与 deployer 的接口兼容性校验（不越界）

## 8. 文档要求

每次变更记录到 `doc/updates/`，至少包含：

- 改了什么
- 为什么改
- 如何验证
- 对 agents 的可见变化

## 9. 故障处理原则

- 先复现，再修复，再回归。
- 对高频问题（重复提醒、消息堆积、协议不一致）优先做机制级修复。
- 用户可见错误必须可诊断，不能只返回模糊失败。

## 10. 交付口径

对外汇报需包含：

- 变更文件
- 行为变化
- 测试结果
- 未覆盖风险
