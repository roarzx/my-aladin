# C11 验收证据 — Flow 节点 Worker（执行侧）

实现：`internal/daemon/flownode.go`、`internal/daemon/cliclient.go`（Flow 节点客户端）、
`internal/flow/claim_next.go` 与 `internal/flow/http.go`（机器侧领取 API）。

- Worker 周期调用 `/api/v1/cli/flow-run-nodes/claim` 领取 agent_prompt 节点
- 执行期间每 10s 续约；续约被拒（旧 epoch / 过期 / 取消）时取消 Provider 并回 cancel-ack
- 成功上报 `complete`（含 summary），失败上报 `fail`
- 服务端 `ClaimNext` 跨运行扫描，`Renew/Accept` 校验 owner/epoch；
  旧 claim 的完成上报返回 409

```bash
go test -race ./internal/daemon -run TestFlowNode -v
go test -tags=integration ./internal/flow -run TestFlowNodeMachineAPI -v
```

结果：

- `TestFlowNode`：领取 → fake ACP 执行 → complete，summary=hello
- `TestFlowNodeFailure`：Provider 启动失败 → fail 上报
- `TestFlowNodeLeaseLost`：续约失败触发取消与 cancel-ack
- `TestFlowNodeMachineAPI`：claim/heartbeat/complete 契约；旧 epoch complete 被拒；
  节点完成后 mars_acceptance 推进，FlowRun succeeded
