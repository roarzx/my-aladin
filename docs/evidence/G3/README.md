# G3 验收证据 — FlowRunner 固定节点

实现：`internal/flow/runner.go`

- `RunOnce`：queued→running，按 ordinal 执行 Server 侧节点
  （environment_prepare / mars_acceptance）；agent_prompt 留给机器领取
- 节点失败 → 节点 failed、run → failed；全部节点完成 → run → succeeded
- 节点顺序来自冻结快照，模板不可配置

```bash
go test -tags=integration ./internal/flow -run 'TestRunnerOrderAndSuccess|TestRunnerFailure' -v
```

结果：env 完成后停在 agent_prompt；机器完成 agent_prompt 后 mars 执行、run succeeded；
注入失败节点后 env failed、run failed。

已知限制：v1 的 environment_prepare / mars_acceptance 为 Server 侧占位实现
（记录结果与顺序），真实命令执行留在后续接入。
