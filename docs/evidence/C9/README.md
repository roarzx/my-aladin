# C9 验收证据 — 上下文同步与版本缓存

```bash
go test ./internal/daemon -v -count=1 -run TestContextSync
go test ./internal/httpapi -v -count=1 -run TestCLI
go test -tags=integration ./internal/store/mysql -run TestContextVersionCAS -v
```

结果：客户端 2 + 端点 4 + 存储 1 个用例通过：

- **CAS 写入**：`expectedVersion` 落后 → 409 `CONFLICT`；等于当前版本 → 新版本
- **冲突合并**：本地落后时拉取远端、调用 `merge(remote)` 后重试一次，版本继续递增
- 本地 `.context_versions.json` 同步更新（键为 `decisions/<task>`、`summary/<step>`）
- 未认证 → 401；缺 `scopeId` → 400；缺失版本 → 404

真实 server 端到端（Docker MySQL）：

```
PUT  /api/v1/cli/contexts/task-e2e/task/files/decisions  {"content":"hello","expectedVersion":0}
     → 200 {"data":{"version":1}}
GET  .../files/decisions/1?scopeId=task-e2e → 200 content=hello
PUT  同一 expectedVersion=0 → 409
GET  .../files → [{"kind":"decisions","version":1}]
无令牌 → 401
DB: context_entries 行存在且 content_hash 非空
```

端点按 9.6.1 挂载，统一机器令牌鉴权；服务端存储为 `context_entries` 的追加版本。
