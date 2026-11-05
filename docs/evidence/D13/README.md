# D13 验收证据 — 事件流与游标

```bash
go test -tags=integration ./internal/store/mysql -v -count=1 -run TestActivity
```

结果：

- `AppendActivity` 在同一事务内递增 `tasks.activity_seq` 并写入事件：seq 单调（1、2…）
- 游标语义为 **`seq > after`**（允许有洞），`after=1` 只返回第二条
- payload 原样存储（JSON）
- 已发出的关键事件：`task.created`、`plan.submitted`、`step.dispatched`、`step.done`、`step.fail`、`step.retried`

端到端（真实 server）：

```
GET /issues/{id}/events?after=0
{"data":{"data":[
  {"seq":1,"kind":"task.created",...},
  {"seq":2,"kind":"plan.submitted",...}
],"nextSeq":2}}
```

端点按任务 Workspace 做成员校验；非成员返回 404（不泄漏存在性）。
