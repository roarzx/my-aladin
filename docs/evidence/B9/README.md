# B9 验收证据 — 事件去重与可靠重投

```bash
go test ./internal/transport/ws -v -count=1 -run 'TestResend|TestAck'
go test -tags=integration ./internal/store/redis -v -count=1 -run TestUnackedStore
```

结果：单元 2 个 + 集成 1 个用例通过：

- 未确认消息按阈值重投，**重投使用原始字节，message id 不变**
- 已 ack 的消息不再重投
- Redis 存储验证：`Track` 两条 → `Due` 返回两条原始 payload → `Ack` 一条后只剩另一条
- 重投间隔、阈值、批量上限均可配置
