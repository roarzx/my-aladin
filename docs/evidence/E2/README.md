# E2 验收证据 — 上下文注入预算

实现：`internal/agentctx/budget.go`（`Fit(bundle, window)`）

- 估算：`EstimateTokens`（约 4 rune/令牌）
- 只按 3.5 优先级裁剪：先历史（2），再最近交互（4）；约束/验收/决策/当前步骤为 0 级，永不裁
- 0 级本身超预算 → `ErrBudgetUnsatisfiable`（调用方暂停并告警，禁止静默丢弃）
- 每次裁剪写入 `Bundle.Trimmed{level, bytes}`，可发 `budget.trimmed` 审计事件

```bash
go test ./internal/agentctx -run TestFit -v
```

结果：预算内不裁剪；裁剪只裁 2→4 且顺序正确；0 级内容完整保留；0 级超限报错。
