# C6 验收证据 — 运行时探活

```bash
go test ./internal/transport/acp -v -count=1 -run 'TestValidate|TestVersionMismatch|TestInitializeFailure'
```

结果：

- Provider 起不来 / `initialize` 失败 → `RUNTIME_START_FAILED`，不创建会话
- 版本不匹配（期望 9.9.9，实际 0.94.0）→ `RUNTIME_START_FAILED`
- `Validate` 成功时立即关闭子进程

真实适配器冒烟（参考实现已安装的 `claude-agent-acp 0.94.0`，仅 initialize，不调用模型）：

```bash
ALADDIN_ACP_BIN="$HOME/.hercules/agents/claude-agent-acp/node_modules/.bin/claude-agent-acp" \
ALADDIN_ACP_VERSION=0.94.0 \
go test -tags=realacp ./internal/transport/acp -run TestRealAdapterValidate -v
```

```
--- PASS: TestRealAdapterValidate (0.27s)
```

版本锁定：配置 `agent.claude.expected_version` 默认 **0.94.0**（与参考实现当前安装一致）；npm 上最新稳定版为 0.100.0，升级时同步修改配置。

已知边界：`initialize` 成功不证明已登录；未登录/鉴权失败会在 `prompt` 阶段以错误或 `stopReason` 暴露，按 9.7.1 的 `uncertain` 路径处理。
