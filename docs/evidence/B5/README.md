# B5 验收证据 — FakeRuntime

```bash
go test ./internal/testkit/fakeruntime -v -count=1
```

结果：6 个用例通过。回放脚本：`tests/fixtures/scripts/basic.yaml`。

覆盖三种故障注入（B5 验收项）：

| 脚本 | 故障 | 结果 |
|---|---|---|
| `exec-ok` | 正常完成 | 事件按序回放（stream_delta → tool_call），状态 `completed` |
| `exec-fail` | 返回失败 | 状态 `failed`，`error_class=tool` |
| `exec-hang` | 超时不响应 | 保持 `running`；`Cancel` 后 `cancelled`，重复取消返回 false |
| `exec-dispatch-error` | 派发阶段断连 | 返回 `ErrDispatch` |

另有能力上报（`Capabilities`/`SetCapabilities`）与未知执行报错用例。
FakeRuntime 只回放归一化事件，不依赖任何 Provider 原始报文（9.7.1）。
