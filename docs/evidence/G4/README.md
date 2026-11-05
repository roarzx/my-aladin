# G4 验收证据 — Flow 节点领取服务端

实现：`internal/flow/claim.go`（执行侧 Worker 属 C11，本包只做服务端）

- `Claim(runID, machineID, ttl)`：只领取 agent_prompt 节点；同一时刻一台机器持有
- 租约过期可被重新领取，epoch 递增；旧 epoch 的 `Renew` / `Accept` 被拒绝
- `Accept` 校验 owner/epoch；取消后的流程不接受节点成功回调（不复活）
- 辅助：`Renew` 仅在租约未过期时延长

```bash
go test -tags=integration ./internal/flow -run TestNodeClaim -v
```

结果：重复领取 → ErrNodeLeased；旧 epoch 续约/上报 → ErrLeaseLost；
过期后重新领取 epoch=2；接受后 mars 推进、run succeeded。
