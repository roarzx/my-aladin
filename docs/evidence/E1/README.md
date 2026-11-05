# E1 验收证据 — 上下文版本仲裁

实现：`internal/agentctx/arbitrate.go`

- `CompareAndPut(ctx, client, entry, expectedVersion)`：版本不匹配返回 `ErrVersionConflict`
- `MergeAndPut(...)`：冲突时重读远端 → `merge(remote)` → 以远端版本重试一次；
  merge 为 nil 时绝不隐式覆盖
- Daemon 的 `ContextSync.Push` 改为走 `MergeAndPut`（`ErrContextConflict` 与
  `agentctx.ErrVersionConflict` 是同一哨兵），服务端存储层仍由唯一键兜底

```bash
go test ./internal/agentctx -run TestArbitrate -v
go test ./internal/daemon -run TestContextSync -count=1
```

结果：CAS 成功、过期版本冲突、冲突后重读合并成功、持续冲突不覆盖、daemon 同步回归通过。
