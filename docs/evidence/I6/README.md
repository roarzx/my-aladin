# I6 验收证据 — 证据归档

- 索引：[`docs/evidence/README.md`](../README.md)，覆盖全部已实现工作包
- 每个目录包含复现命令、结果与限制说明；集成/e2e/perf 用例均在 Docker 依赖下验证
- 抽检示例：
  - D9：`go test -tags=integration ./internal/orchestration -run TestStepDone`
  - G4：`go test -tags=integration ./internal/flow -run TestNodeClaim`
  - I1：`make test-e2e`

抽检 3 项均可按文档复现（本机 2026-11-06，MySQL 8 / Redis 7）。
