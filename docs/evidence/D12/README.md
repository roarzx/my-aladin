# D12 验收证据 — 步骤摘要同步（digest 判重）

## 实现位置

CAS 与唯一键约束的权威实现位于 `internal/store/mysql/context.go` 的
`PutContextVersion`（spec 中的 `internal/agentctx/store.go` 未单列薄封装：
版本仲裁必须与 INSERT 处于同一存储层，否则并发写入无唯一键兜底）。

Daemon 侧上报沿用 C9 的 `ContextSync.Push`（`internal/daemon/contextsync.go`）：
携带 `expectedVersion`，冲突时拉取远端、调用 merge 后以远端版本重试一次。

本包新增：**相同 `content_hash` 重复上报不升版本**——直接返回当前版本与内容，
不再插入新行；digest 不同则版本 +1。

## 验证

```bash
go test -tags=integration ./internal/store/mysql -run TestContextVersionCAS -v -count=1
```

结果（真实 MySQL）：

- 首次写入 → 版本 1
- 过期 `expectedVersion` → `CONTEXT_CONFLICT`
- 写入 v2 → 版本 2
- **重复上报 v2（同 digest）→ 仍为版本 2，`MAX(version)` 不增长**
- 读取/列表版本接口返回正确内容与 hash

## 已知限制

- 摘要内容由执行侧（Daemon）生成；服务端只做版本与 digest 仲裁，不校验摘要语义。
- 大日志不进上下文（§5.8 不变量）：上报方只发摘要，正文走文件路径。
