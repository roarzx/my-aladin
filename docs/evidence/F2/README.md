# F2 验收证据 — WorkItem 模型与状态

实现：`internal/domain/workitem.go`、`internal/store/mysql/workitem.go`

- 状态：proposed / reviewing / awaiting_approval / ready / running / blocked / done / cancelled
- 事件：review_started / approval_requested / approved / rejected / started / blocked /
  unblocked / completed / cancelled
- `CanTransition/Apply` 拒绝跳级与非法转换；终态不可再启动
- `StartWorkItem` 在事务内锁任务行，保证同一 Issue 最多一个 running

```bash
go test ./internal/domain -run TestWorkItemTransition -v
go test -tags=integration ./internal/squad -run TestStartWorkItemSingleRunning -v
```

结果：合法链 proposed→…→done 通过；proposed→completed 被拒；驳回回到 reviewing；
两个 ready 工作项只有一个能进入 running，前一个结束后第二个才能启动。
