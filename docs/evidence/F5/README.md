# F5 验收证据 — 方案版本与审批链

实现：`internal/squad/proposal.go`、`internal/store/mysql/proposal.go`

- `Propose`：生成新 revision（hash 落库）；有审批链时 WorkItem → awaiting_approval，
  无审批时直接 ready
- `Approve`：校验 revision、审批顺序、审批人；必须按 order 依次通过；
  全部通过后 WorkItem → ready
- `Reject`：工作项回到 reviewing
- 修订产生新 proposal，旧 proposal 的审批不能授权新版本（旧审批被拒绝）

```bash
go test -tags=integration ./internal/squad -run TestApprovalChain -v
```

结果：跳级 → CONFLICT；越权 → FORBIDDEN；顺序通过后 ready；
修订后审批旧方案 → PLAN_SUPERSEDED。
