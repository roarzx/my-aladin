# D3 验收证据 — 流式接收与状态机

```bash
go test ./internal/state -v -count=1
```

结果：3 个用例通过：

- 按 seq 顺序应用，文本正确拼接，final 置位
- **乱序到达**：seq 小于等于当前值的片段被丢弃并计数，不污染已渲染内容
- **`final=true` 之后的内容全部丢弃**（包括 seq 更大的片段）
- 快照包含 seq、文本、final、丢弃数与应用数

状态机只依赖事件本身，不依赖消息历史；CLI 侧的 `stream/state_change` 由 D3 接入。
