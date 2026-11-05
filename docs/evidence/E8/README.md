# E8 验收证据 — 背压处理

实现：`internal/transport/ws/backpressure.go`、`internal/store/redis/backpressure.go`

- 会话级待处理字节：`bp:{session_id}` Hash（`pending`、`paused_since`，TTL 60s）
- `Check(sessionID)` → continue / pause / terminate：
  超过 High 暂停并记录暂停时刻；持续超过 MaxPause 终止；超过 Max 立即终止；
  降到 Low 以下清除暂停标记（带迟滞）
- `Add`/`Ack` 维护计数并返回当前动作

```bash
go test ./internal/transport/ws -run TestBackpressure -v
```

结果：低水位 continue、高水位 pause、持续超时 terminate、硬上限 terminate、
迟滞区间保持 pause、回落后恢复。

已知限制：daemon→server 流量的实际背压仍由 C7 的 `Streamer` 有界缓冲执行；
本组件是服务端会话级守卫（供后续广播/多消费者场景接入），当前未插入消息主路径。
