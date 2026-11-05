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

## 真实 daemon + ACP 闭环（D13 批次补充）

此前编排闭环由 stub Runtime 驱动。本批次补上 daemon 侧的真实执行：

- `internal/daemon/step_handler.go`：处理 `start_step_session`，拉起 ACP Provider、
  执行 prompt、流式回传 `stream_delta`，终态发 `step_done` / `step_fail`
- `internal/daemon/sender.go`：命令处理过程中的连接级 Sender（事件 + 流）
- `internal/daemon/boot.go`：用 `agent.claude` 配置构建 StepHandler，经命令幂等
  Dispatcher 包装；`agent.claude.bin` 可直接指向测试用 fake agent
- `plan.Service.Start` 分配计划分支 `aladdin/<taskid8>`（`SetTaskPlanBranch` 幂等写入）

端到端验证（真实 server + 真实 daemon + fake ACP agent，MySQL/Redis 为 docker）：

```
task_status=chatting
plans.status=completed
plan_steps.status=completed
attempts.status=completed (attempt_no=1)
activities: task.created → plan.submitted → step.dispatched → step.done
tasks.plan_branch=aladdin/a04d7cd4
```

新增测试：

- `go test ./internal/daemon -run TestStepHandler`：4 个用例（成功闭环、启动失败
  → `step_fail` class=runtime、未知方法拒绝、无回传通道拒绝）
- `go test -tags=integration ./internal/plan -run TestProposeConfirmStart`：
  断言启动后 `PlanBranch` 已持久化
