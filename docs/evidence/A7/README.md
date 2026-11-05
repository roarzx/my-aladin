# A7 验收证据 — Redis 键封装与锁

```bash
make up
go test -tags=integration ./internal/store/redis -v -count=1 -run 'TestLock|TestKeys'
```

结果：`ok`，验证内容：

- `Keys` 统一生成 9.4 的键：`plan:` / `lock:task:` / `lease:attempt:` / `recovery:dirty` / `bp:`
- 获取锁：`SET NX PX`
- 续约与释放：Lua 原子比较持有者（禁止无条件 DEL / 先读后写）
- 非持有者续约、释放均失败且不删除锁
- TTL 到期后可被其他持有者获取
- 空 owner、零 TTL 被拒绝
