# B7 验收证据 — WS 消息编解码

```bash
go test ./pkg/protocol -v -count=1
```

结果：17 个用例通过，覆盖 9.7 的信封与执行类围栏：

- 版本必须是 `2.0`；未知 `type` 拒绝；未知字段拒绝（`DisallowUnknownFields`）
- 执行类消息必须同时带 `execution_id` 与 `execution_attempt`：
  - `stream`（且必须带 `stream{reply_id,seq}`）
  - `command.start_step_session`
  - `event.step_done` / `event.step_fail`
  - `response`（携带 `execution_id` 时）
- 非执行类消息（`start_planning_session`、普通事件/响应、心跳）不要求围栏字段
- 非法 payload JSON 在编码阶段被拒绝
- 编码时补齐 `ts` 与默认版本
