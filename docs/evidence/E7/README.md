# E7 验收证据 — 错误分类

实现：`pkg/errcode/classify.go`

- `Classify(err)` → network / runtime / tool / verification / permission：
  错误码优先（MachineOffline/ResultUnknown/UpstreamUnavailable→network，
  Forbidden/Unauthenticated/AgentUnavailable→permission，
  VerificationIncomplete→verification，RuntimeStartFailed→runtime），
  其次 context 超时、net.Error、权限错误、`exec.ExitError`
- `Policy(class)` 对应 3.4 重试策略：permission 不重试；verification 仅人工；
  network 预算内自动；runtime/tool 自动 1 次
- ACP prompt 超时改为同时包装 `ErrPromptTimeout` 与 `ResultUnknown` 错误码，
  StepHandler 用 `Classify` 生成 `step_fail.error_class`

```bash
go test ./pkg/errcode -run 'TestClassify|TestPolicy' -v
```

结果：13 个分类用例与 4 条策略断言通过。
