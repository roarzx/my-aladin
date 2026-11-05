# B10 验收证据 — 机器注册与能力上报

```bash
go test ./internal/machine -v -count=1
go test -tags=integration ./internal/store/mysql -v -count=1 -run TestMachineRegistry
```

结果：单元 3 个 + 集成 1 个用例通过：

- 能力 JSON 的编码/解析与 `Supports`
- 注册（upsert）：上线、能力、CLI 版本、`last_seen_at`
- **未声明的能力查询返回 false**（对应"未声明 attachments 的机器不下发附件命令"）
- 心跳刷新 `last_seen_at`
- 状态切换（`disabled`），重复注册更新能力
- 不存在的机器心跳返回 `sql.ErrNoRows`
