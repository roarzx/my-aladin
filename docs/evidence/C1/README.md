# C1 验收证据 — Daemon 进程入口与 Profile

```bash
go test ./internal/daemon -v -count=1 -run 'TestResolveProfile|TestBackoff'
```

结果：

- 同一 profile 重启后 `machine_id` 保持不变（落盘在 `<profile_dir>/<profile>/machine_id`，权限 0600）
- 显式 `--machine-id` 优先；不同 profile 生成不同 machine_id
- 空名称、含路径分隔符的名称被拒绝
- 指数退避：首次约 10ms，封顶 40ms（±10% 抖动）

双实例并行（真实 server + 两个 daemon）：

```
server_accepted=2  d1=1  d2=1
/tmp/aladdin-profiles/
  dev1
  dev2
```

`daemon --profile dev1 --machine-id m-dev-1` 与 `--profile dev2 --machine-id m-dev-2` 同时连接，互不干扰。
