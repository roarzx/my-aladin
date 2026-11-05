# C12 验收证据 — 进程回收

```bash
go test ./internal/daemon -v -count=1 -run 'TestJanitor|TestStopAll|TestCleanupStale|TestWait'
```

结果：4 个用例通过：

- 子进程在独立进程组中启动；`StopAll` 整组 SIGKILL，`Wait` 回收后登记清空
- **kill -9 daemon 后子进程被清理**：helper 充当 daemon（启动 `sleep 60` + janitor），测试对 helper 发送 SIGKILL，janitor 通过管道 EOF 感知并杀掉遗留进程组
- **PID 复用不误杀**：登记身份（`ps -o lstart`）与当前进程不一致时跳过
- 正常退出：登记文件被移除

真实 daemon 的优雅退出（`kill -TERM`）：

```
connected=1
daemon_exited=yes
daemon_procs_left=0
profile_files=commands.jsonl
```

修复项：SIGTERM 时客户端阻塞在 `ReadMessage` 上，ctx 取消未能关闭连接，导致 daemon 要等心跳超时才退出；现在 ctx 取消会立即关闭连接，janitor 也会随之退出并清理 `children.json`。
