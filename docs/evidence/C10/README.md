# C10 验收证据 — 工作区与 Git 操作

```bash
go test ./internal/daemon -v -count=1 -run 'TestPrepare|TestDirty|TestPush|TestDiff'
```

结果：5 个用例通过（本地 bare 仓库 + 真实 git）：

- 克隆并创建/切换到计划分支
- **HEAD 与基线不一致** → `ErrHeadMismatch`（错误信息带两者 SHA）
- **脏工作区不被清理**：`EnsureClean` 返回 `ErrDirtyWorkspace`；再次 `Prepare` 后用户改动仍在
- **只允许推送计划分支**：推 `main` → `ErrBranchNotAllowed`；推计划分支成功，远端可验证
- `Diff` 与 `Commits` 返回区间内改动与提交

实现基于 git CLI，不引入额外依赖；推送使用稳定分支名，后续与 3.4 的对账规则配合。
