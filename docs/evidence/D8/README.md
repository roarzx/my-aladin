# D8 验收证据 — Step 派发

```bash
go test -tags=integration ./internal/orchestration -v -count=1 -run TestRunOnce
```

结果：2 个用例通过（真实 DB + stub Runtime）：

- 首次 `RunOnce` 派发第一个 pending 步骤：步骤 → `executing`，attempt 落库（attempt_no=1，execution_id 与派发请求一致）
- **存在 `executing` 步骤时再次 RunOnce 不再派发**（串行约束）
- 完成第一步后，下一次 RunOnce 派发第二步
- **派发前重校验 Agent 可调度性**：私有且属他人的 Agent → 步骤 → `failed`，不调用 Runtime，返回 `AGENT_UNAVAILABLE`

可选的任务锁：`PlanRunner.Lock` 非空时先抢锁，抢不到直接跳过本轮。

## 接线（D9–D13 批次补充）

- `ws.Hub` 维护 `machineID → 会话`，实现 `runtime.Runtime`（`Dispatch` 下发 `start_step_session`）与 `session.Dispatcher`（`start_planning_session`）
- 服务端启动 `orchestration.Loop`（1s 周期）扫描 `executing` 任务并调用 `PlanRunner.RunOnce`
- daemon 上行的 `step_done` / `step_fail` 经 `Hub.OnEvent` 交给 `EventHandler` 推进状态
- `PlanRunner` 派发前把 Agent 的 `machine_id` 写入 `DispatchRequest`
