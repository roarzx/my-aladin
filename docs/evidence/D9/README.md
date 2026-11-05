# D9 验收证据 — step.done 校验与计划推进

```bash
go test -tags=integration ./internal/orchestration -v -count=1 -run 'TestStepDone|TestLate'
```

结果：

- `step_done` 到达后：attempt → `completed`（写入 digest/commit），Step → `completed`
- 还有 pending 步骤时自动派发下一步（串行）
- 全部步骤终态且无失败 → Plan `completed`、Task `chatting`
- **迟到/串号结果被拒绝**：`attempt_no` 与当前 attempt 不符时不推进状态
- 未知 execution 的 done 只记日志，不影响任何状态

校验点：execution_id → attempt 匹配、attempt_no 匹配、Step 仍为 `executing`（终态不可变）。
