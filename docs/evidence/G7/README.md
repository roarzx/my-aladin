# G7 验收证据 — Skill / MCP / Plugin 治理

实现：`internal/capability/registry.go`

- `Catalog.Put/Resolve`：维护 Agent 能力清单（attachments / git / flow_node + tools）
- `Authorize(agentID, tool)`：未知 Agent 明确报错；能力位不可用 → `ErrUnavailable`
  （不静默降级）；未授权工具 → `ErrUnauthorized`；结果写入审计回调
- `FilterTools`：派发前剔除未授权工具并返回 removed 列表

```bash
go test ./internal/capability -run TestAuthorize -v
```

结果：授权/拒绝/能力不可用/未知 Agent 均按预期；过滤器剔除 shell，保留 search/git_diff；
审计记录包含拒绝项。

已知限制：尚未接入 daemon 派发前的工具清单裁剪（当前协议未携带工具参数），
授权组件已就绪，接入点留在协议扩展时。
