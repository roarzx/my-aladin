# E10 验收证据 — 故障注入套件（普通模式）

实现：`tests/integration/faults_test.go`（真实 MySQL/Redis）

```bash
go test -tags=integration ./tests/integration -run TestFault -v
```

7 个场景全部通过：

1. 确认后修改 Plan → 启动被拒（`NOT_CONFIRMED`）；重复确认不增加审批效果
2. 两个 Runner 同时抢占 → 只产生一次派发、一条 attempt
3. 状态已提交但 WS 未送出 → 游标补拉返回全部事件；seq 有洞仍按 `> after` 读取
4. Daemon 已完成但回调丢失 → 先取证；无 Git 证据转人工，保持 reconciling，不盲目重跑
5. 租约过期后旧执行回调 → 非最新 attempt 的 step_done 被拒绝，新 attempt 不变
6. 取消与成功竞争 → 取消落终态，迟到 step_done 不复活任务
7. （场景 8）上下文压缩保留约束/验收/决策，决策文本带证据 ID 可定位原始证据

未覆盖场景及归属：7→F、9→B、10→C、11/12→G（文件头已注明）。

配套改动：集成测试以 `-p 1` 串行运行（共享同一 MySQL/Redis，避免跨包干扰）。
