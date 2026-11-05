# B0 验收证据 — HTTP 基座

```bash
go test ./internal/httpapi -v -count=1
```

结果：8 个用例通过，覆盖：

- `/healthz` 不经过认证（探针可用）
- 未知路由返回统一错误体 `{"error":{"code":"NOT_FOUND",...}}`
- 缺令牌 → 401 `UNAUTHENTICATED`；viewer 创建 → 403 `FORBIDDEN`
- member 创建 → 201 + `{"data": ...}` 成功信封
- 非成员访问 `/workspaces/{wid}` → 403
- handler panic → 500 `INTERNAL_ERROR`，进程不退出
- `X-Request-ID` 透传，未提供时生成；同时生成 `X-Trace-ID`

实机验证（Docker MySQL/Redis）：

```
healthz=200 readyz=200 nope=404
{"error":{"code":"NOT_FOUND","message":"路由不存在"}}
```

同时把 `UNAUTHENTICATED`(401) 与 `INTERNAL_ERROR`(500) 补进了 9.6 错误码表（原表缺少认证失败与内部错误）。
