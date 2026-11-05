# G1 验收证据 — FlowTemplate 发布与版本

实现：`internal/flow/template.go`、`internal/store/mysql/flow_template.go`

- `Publish`：expectedVersion 必须等于当前最新版本（首次 0），否则 `REVISION_CONFLICT`；
  每次发布插入新版本行（version+1），旧版本保留
- 节点固定为 environment_prepare → agent_prompt → mars_acceptance，顺序不可配置
- `Disable` 停用；`Delete` 仅允许删除已停用版本

```bash
go test -tags=integration ./internal/flow -run TestPublish -v
```

结果：v1/v2 递增、旧版本保留；错误 expectedVersion → 冲突；非法节点顺序被拒；
启用中删除 → CONFLICT；停用后可删除。
