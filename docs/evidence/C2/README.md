# C2 验收证据 — 控制面连接（握手 / 心跳 / 重连）

```bash
go test ./internal/daemon -v -count=1 -run 'TestClient'
```

结果：4 个用例通过（全部基于 C13 的 FakeServer）：

- 握手成功并持续发送心跳
- 服务端断开后自动重连（短退避配置下 10ms 级）
- 服务端返回 401 时停止重试并返回 `ErrRejected`（不会无限重连）
- 收到 command 后回传同 id 的 response

真实 server + daemon 端到端：

```
connect1=1  server_accepted=1
kill server → 准备重连
restart server → connect2=2  server2_accepted=1
```

断线到重连成功的等待约 **0.49s**（远小于 30s 验收线）；握手包含 `machine_id`、`cli_version`、能力列表，客户端发送心跳、服务端回 `handshake_ack`。

鉴权：WS 升级前校验 `Authorization: Bearer <machine_token>`，服务端用 `machine_tokens` 的 sha256 哈希查找；令牌不存在/过期/撤销均 401。令牌签发见 `aladdin machine token --machine-id ...`。
