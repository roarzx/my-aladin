# E6 验收证据 — 对账流程

实现：`internal/recovery/{evidence,reconcile,http}.go`

- `Reconcile(ctx, stepID)`：取证 → 判定表 → 裁决；判不了保持 `reconciling` 并记审计
- 判定表五情形单测：有新提交+摘要+digest 一致→completed；无提交+无摘要→failed；
  有提交+无摘要→undecidable；机器在线且执行中→**不抢**；证据不足→undecidable
- 自动裁决落状态：completed → attempt/Step completed 并推进 Plan；failed → Step failed、
  Plan/Task paused
- 人工裁决：`POST /steps/{id}/reconcile`（`{verdict, evidence}`），写
  `attempts.reconcile_verdict`，仅接受 `step=reconciling`
- `reconciling` 期间 Retry 仍由既有 `STEP_NOT_FAILED` 规则拒绝（状态非 failed）

```bash
go test ./internal/recovery -run TestDecideTable -v
go test -tags=integration ./internal/recovery -run 'TestReconcile|TestApplyManualVerdict' -v
```

结果：五种判定、自动落 completed/failed、undecidable 保持冻结、人工裁决写库均通过。

已知限制：`DBSource` 目前只能取证摘要与执行状态；Git 证据（get_git_diff /
list_git_commits）尚未在普通模式实现，因此 `GitEvidenceKnown=false` →
自动判 completed/failed 需要注入提供 Git 证据的 `EvidenceSource`（接口已留）。
