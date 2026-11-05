# D10 验收证据 — step.fail 与 Plan 暂停

```bash
go test -tags=integration ./internal/orchestration -v -count=1 -run 'TestStepFail'
```

结果：2 个用例通过：

- 普通失败：attempt → `failed`（记录 `error_class`），Step → `failed`，Plan → `paused`，Task → `paused`
- **结果未知**（`uncertain=true`）：attempt → `unknown`，Step → `reconciling`，Plan/Task 暂停，等待对账
- 同一失败重复上报幂等（Step 已非 executing 时直接忽略）
- 迟到/串号失败被拒绝

失败不会自动重试；重试走 D11 的人工路径（后续 E 阶段接地对账后再决定是否自动）。
