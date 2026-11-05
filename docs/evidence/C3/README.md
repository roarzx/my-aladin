# C3 验收证据 — 命令分发与幂等

```bash
go test ./internal/daemon -v -count=1 -run 'TestDispatcher|TestIdempotent'
```

结果：7 个用例通过：

- 重复命令不再调用 handler，直接返回上次结果
- **同一 id 并发 5 次只执行一次**（inflight 合并）
- handler 返回错误时不缓存，允许重试
- **daemon 重启后同一 command id 仍命中缓存**（`commands.jsonl` 落盘）
- 过期记录（默认 24h）不再命中
- 同 id 多次写入以最后一次为准

实现：`commands.jsonl` 追加写 + fsync；启动时扫描并按 id 去重、清理过期与损坏行；LRU 不参与正确性。daemon 启动日志会打印 `known_commands`。
