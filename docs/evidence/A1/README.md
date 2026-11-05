# A1 验收证据 — Go module 与可运行入口

- 日期：2026-11-05
- 环境：macOS（darwin/arm64），Go 1.24.5

```bash
make build
./bin/server --health
go vet ./...
```

结果：

- `make build` 产出 `bin/server`、`bin/daemon`、`bin/aladdin`
- `./bin/server --health` → `server: ok (0.1.0-dev (unknown))`
- `go vet ./...` 通过（无输出）
