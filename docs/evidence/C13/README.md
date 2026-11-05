# C13 验收证据 — FakeServer 测试夹具

`internal/testkit/fakeserver` 提供可脚本化的 WS 对端，能力：

- 可配置期望的 Bearer 令牌；不匹配或 `Reject=true` 时返回 **401**
- 完成握手并回 `handshake_ack`；记录 `Connected` 信号
- 向客户端广播命令（`Send`），捕获客户端发来的全部信封（`Received`）
- `Drop()` 主动断开当前连接，用于验证重连

C2 的 4 个用例（握手心跳、重连、拒绝、命令响应）**全部基于 FakeServer 通过**，未依赖真实控制面：

```bash
go test ./internal/daemon -v -count=1 -run 'TestClient'
```

C3/C7 的测试将复用同一夹具。
