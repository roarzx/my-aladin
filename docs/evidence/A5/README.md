# A5 验收证据 — 状态转换守卫

```bash
go test ./internal/domain -v -count=1 -run 'Transitions|ApplyIncrementsVersion'
```

结果：57 个用例通过，覆盖 dev_spec 9.1 的三张转换表：

- Task：创建、提交、确认启动、修订、失败、对账、完成、重试、取消三态、追加计划
- Plan：proposed/started/finished/stopped、步骤失败与对账暂停
- Step：选中、完成、可重试失败、终态失败、派发失败重试、结果未知、对账三出口、人工重试、取消
- 反例：终态重派、`reconciling` 期间 Retry、未知事件、未创建任务直接提交
- `Apply` 成功后 `Version` 递增，`UpdatedAt` 被设置
