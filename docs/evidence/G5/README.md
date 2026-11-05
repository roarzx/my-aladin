# G5 验收证据 — 节点重跑与下游失效

实现：`internal/flow/rerun.go`，迁移 `0004_flow_stale`
（`flow_node_runs.status` 增加 `stale`）

- `Rerun(nodeID)`：仅 failed 节点可重跑；节点 → pending、attempt_no+1；
  下游节点 → stale，清空结果与租约；run → running
- `RunOnce` 把 stale 当作待重算；agent_prompt 的 stale 可被重新 Claim
- `Cancel` 只允许 queued/running；取消后的节点成功回调不复活流程

```bash
go test -tags=integration ./internal/flow -run TestRerunInvalidatesDownstream -v
```

结果：重跑 env 后 agent/mars 变 stale；重新执行全链路后 run succeeded；
取消后的接受路径被拒绝（见 G4 的 Claim/Accept 测试与 `Cancel` 实现）。
