# I2 验收证据 — E2E 小队模式

实现：`tests/e2e/squad_test.go`，场景夹具 `tests/fixtures/squads/approval_chain.yaml`

流程：创建小队任务（快照）→ 工作项方案 → 跳级审批被拒 → 顺序审批 →
修订方案使旧审批失效 → 重新审批 → SquadRunner 启动 → 完成第一项 →
第二项审批后接续；同一 Issue 不并发推进两个工作项。

```bash
make test-e2e
# 或：go test -tags=e2e ./tests/e2e -run TestSquad -v
```

结果：两级审批顺序通过；修订后旧审批返回 `PLAN_SUPERSEDED`；
同一时刻只有一个 running 工作项；第二项完成审批后由 Runner 派发。
