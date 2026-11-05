# F6 验收证据 — 小队会话与 thread 复用

实现：`internal/session/squad_session.go`、`internal/store/mysql/session.go`

- `ReuseOrCreate(taskID, agentID)`：用 Agent 的执行语义（能力集、环境变量、
  System Prompt、工作目录、机器）计算指纹
- 指纹匹配且已有 `acp_session_id` → 复用同一记录（返回 reused=true）
- 指纹不匹配 → 新建 `session_ordinal + 1`，旧会话保留

```bash
go test -tags=integration ./internal/session -run TestSquadReuse -v
```

结果：首次创建 ordinal=1；记录 ACP 会话后同配置复用；修改 System Prompt 后
新建 ordinal=2 且不复用旧 ACP 会话。
