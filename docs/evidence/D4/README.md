# D4 验收证据 — Reply Activity 跟踪

```bash
go test -tags=integration ./internal/session -v -count=1 -run TestReplyLifecycle
```

结果：

- 同一 `source_message_id` **只能有一个未终态 reply**（数据库生成列 + 唯一索引强制）
- 重复 Open 返回已有记录（幂等）
- 关闭后 `FindOpenReply` 返回 `ErrNoRows`
- **重复关闭幂等**（不报错、不覆盖终态时间）
- 终态后同一 source 允许开启新回复

记录字段：来源消息、会话、任务、步骤（可空）、execution_attempt、阶段与终态时间。
