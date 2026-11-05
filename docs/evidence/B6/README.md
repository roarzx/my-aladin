# B6 验收证据 — WS 服务端骨架

```bash
go test ./internal/transport/ws -v -count=1 -run 'TestHandshake|TestCommandBefore|TestHeartbeat'
```

结果：3 个用例通过（真实 WebSocket，httptest）：

- **认证**：无令牌升级返回 **401**；正确 Bearer 令牌可升级并完成握手，服务端回 `handshake_ack`
- **握手前置**：握手前发送 `command`，服务端立即断开
- **心跳超时**：握手后保持沉默，超过 `HeartbeatTimeout`（测试用 150ms）服务端断开连接

配置默认值与 9.9 一致：心跳 30s、超时 90s、握手 10s、写超时 5s、单条上限 1 MiB。
