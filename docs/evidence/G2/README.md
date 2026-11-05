# G2 验收证据 — FlowRun 创建与快照

实现：`internal/flow/run.go`、`internal/store/mysql/flow_run.go`

- `CreateRun`：读取并校验模板，冻结模板快照与 inputs，写入 run + 三个固定节点
- 模板后续发布新版本不影响已创建 Run；快照可回读
- 节点 ordinal 固定（1..3），初始 pending、attempt_no=1、lease_epoch=0

```bash
go test -tags=integration ./internal/flow -run TestRunSnapshot -v
```

结果：节点顺序正确；发布 v2 后 GetRun 的 snapshot 仍为 v1；快照内容可解析。
