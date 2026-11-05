# ADR-0003：WS 协议分层与围栏

## 背景

daemon 需要接收命令、回传事件/流，并在断线后可靠补投；执行结果必须可对账。

## 决策

- WS JSONL 信封：`handshake / heartbeat / command / response / event / stream`。
- 执行类消息携带 `execution_id` + `execution_attempt` 围栏；`step_done/fail`
  只接受该步骤最新 attempt。
- 命令/响应按 `id` 配对；未确认响应存 Redis，重连后按原字节重投。
- 事件仅用于审计与推进状态，不承担业务状态权威（以 MySQL 为准）。

## 后果

- 迟到/串号结果被拒绝，取消后的成功回调不会复活任务。
- `stream_delta` 不保证可重放；渲染以最终状态与 `activities` 为准。
