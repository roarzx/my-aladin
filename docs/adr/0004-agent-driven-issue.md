# ADR-0004：Issue 状态改由 Agent 驱动，平台只提供 CLI

## 背景

现有实现把 Issue 的状态机放在服务端：规划 Agent 提交计划 → 人工确认 → `PlanRunner`
每秒扫描并串行派发 step → daemon 收到 `step_done/step_fail` → 平台推进任务/计划/步骤三层状态。
这套模型的根本假设是「平台知道任务什么时候算完成」，于是：

- 只能支持单 Agent 串行；并行、多 Agent 协作没有落点；
- 完成与否取决于 `StopReason == "end_turn"`，回合结束被当成任务完成，容易误报；
- 用户追问要伪造成一个新计划版本，会话不是会话，而是「计划 + 步骤」的投影。

## 决策

1. **Issue 状态由 Agent 驱动。** Agent 在会话里用 CLI 上报，平台不再用 `taskTransitions`
   约束它（`domain.Task.ReportStatus` 只做取值校验与终态保护）。
2. **平台只提供 CLI 这一个写入口。** 命令面与上游 `hercules hercules-openapi` 保持一致，
   前缀改为 `aladdin openapi`：

   ```
   aladdin openapi issue get|list|create|update-status
   aladdin openapi issue input request
   ```

3. **身份与上下文由 daemon 注入。** 一次执行对应一个 Issue，daemon 在拉起 ACP 进程时注入
   `ALADDIN_API_URL`、`ALADDIN_TOKEN`（机器令牌）、`ALADDIN_ISSUE_ID`、`ALADDIN_AGENT_ID`，
   并把 `aladdin` 所在目录放进 `PATH`；提示词末尾追加「平台上报（必须执行）」说明。
4. **上报口径**：`in_progress | blocked | completed | cancelled | failed`，与库内枚举的映射
   集中在 `domain.ParseReportedStatus`。完成/暂停可以回到进行中（用户追问后继续），
   取消/失败是终态，不可复活。
5. **写操作必须有归属**：机器令牌 → 机器 → 工作区；读要求 Issue 与机器同工作区，
   写还要求该 Issue 分配了运行在这台机器上的 Agent（`task_agents ⋈ agents.machine_id`）。

## 后果

- 计划链路已删除：`plans/plan_steps/attempts/step_dependencies/confirmations`、
  `PlanRunner/Retry/Reconcile/Watchdog`、前端计划表单与 `/plans/*` 端点全部移除；
  `internal/orchestration` 只剩小队部分（移至 `internal/squadrunner`）。
- 一轮执行记一条 `issue_runs`：`start_issue_turn` 派发，`turn_done/turn_fail` 收尾；
  会话行（`sessions.kind='issue'`）保存 `acp_session_id`，下一轮用 `session/load` 续上。
- 「Agent 一轮跑完但没上报」时平台不做任何动作（保持进行中），由用户或下一轮对话驱动；
  派发失败会回滚状态并记录 `turn.failed`，避免平台谎报在执行。
- 产物归属改到执行轮次（`artifacts.run_id`）；小队模式的规划/协作会话与 `issue plan/step`
  子命令留待后续评估。
