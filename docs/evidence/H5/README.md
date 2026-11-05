# H5 验收证据 — 评估 Runner

实现：`internal/evaluation/runner.go`，夹具 `tests/fixtures/evaluation/suite.json`

- `Runner.Run(ctx, suite)` 顺序重放固定任务集，逐任务记录结果与证据
- 配置哈希覆盖 suite + 模型/参数，保证同一配置重复运行可对齐
- 执行错误不中断其余任务，失败任务保留 Error 与 Evidence

```bash
go test ./internal/evaluation -run TestRunner -v
go test ./internal/evaluation -run TestSuiteFixtureLoads -v
```

结果：两次同配置运行 ConfigHash 一致、结果顺序稳定；不同配置哈希不同；
失败任务证据保留；夹具可加载。
