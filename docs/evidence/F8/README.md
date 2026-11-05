# F8 验收证据 — 小队 CLI

实现：`cmd/aladdin/squad.go`，子命令：

- `squad action --work-item <id> --action start|complete|block|cancel --actor <id>`
- `squad handoff --work-item <id> --to-agent <id>`
- `squad context --task <id> --agent <id>`（输出 Agent 上下文最新版本 JSON）
- `squad artifact --proposal <id> --offset <n> --limit <n>`（按字节区间读取）

每次动作写 `work_item.action` / `work_item.handoff` 审计事件；
`artifact` 的字节边界会对齐到合法 UTF-8 rune，避免输出半个字符。

```bash
go test ./cmd/aladdin -run TestSliceUTF8 -v
go test -tags=integration ./cmd/aladdin -run TestSquadCommands -v
```

结果：action 推进状态并输出新状态；handoff 更新 owner；context 输出 JSON；
artifact 按区间返回 `你好`；UTF-8 边界对齐与越界校验通过。
