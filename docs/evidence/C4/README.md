# C4 验收证据 — 会话管理与本地历史

```bash
go test ./internal/daemon -v -count=1 -run TestSessionStore
```

结果：3 个用例通过：

- 写入后重新打开 `session_history.json`，映射可读回（Server Session ↔ ACP Session、Agent、指纹）
- 删除后不再命中
- **损坏的历史文件直接报错**，不静默重置
- 写 100 次的同时并发读 200 次，读到的内容始终是完整 JSON；**目录中无残留临时文件**

实现：`writeFileAtomic`（同目录临时文件 → `fsync` → `chmod 0600` → `rename`），读者只会看到旧版本或完整新版本。daemon 启动时会加载并打印 `known_sessions`。
