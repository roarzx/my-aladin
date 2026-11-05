# C7 验收证据 — 流式回传与背压

```bash
go test ./internal/daemon -v -count=1 -run TestStream
```

结果：4 个用例通过：

- 正常 flush 按顺序写出
- **高水位暂停**：缓冲超过 `HighWater` 后 `Append` 阻塞（不继续读取上游）；sink 释放后恢复，数据顺序不变
- **硬上限**：单次追加超过 `MaxBuffer` 立即返回 `ErrBackpressureLimit`
- **持续背压**：sink 阻塞超过 `MaxPause` 时由独立 watchdog 终止，返回 `ErrBackpressureTimeout`；`Append`/`WaitResume` 都拿到同一错误

背压监控与排空在不同 goroutine，sink 阻塞时仍能发现持续背压并终止会话。
