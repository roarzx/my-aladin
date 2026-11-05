# D11 验收证据 — 重试与 attempt 递增

```bash
go test -tags=integration ./internal/orchestration -v -count=1 -run TestRetry
```

结果：3 个用例通过：

- 失败步骤重试：新建 Attempt（attempt_no = 2），Step → `executing`，Plan/Task 从 `paused` 恢复 `executing`，并重新派发
- **非 failed 步骤拒绝重试** → `STEP_NOT_FAILED`
- **超出预算** → `RETRY_BUDGET_EXCEEDED`（`max_attempts_per_step`）
- 派发失败时回滚：Step 回到 `failed`，attempt 标记失败（不丢失败证据）

重试前的授权与可调度性校验复用 D8 的逻辑（agent 白名单、`SchedulableBy`）。
