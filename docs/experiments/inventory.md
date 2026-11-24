# 阿拉丁源码与观测能力清单（P0 审计草稿）

> 路径说明：手册 §0.1 建议的 `experiments/inventory.md` 在本仓库落到 `docs/experiments/inventory.md`，
> 与已有的 `docs/evidence/` 文档体系保持一致。
>
> **审计性质**：只读代码审计，未修改任何业务代码、未连接生产数据、未执行写入命令。
> 本文所有结论分三档标注：**【代码证据】**（可直接用 `path:line` 复核）、
> **【待运行验证】**（静态阅读强指向但未跑实验）、**【未确认】**。

- 审计日期：2026-11-24
- 仓库：`github.com/roarzx/my-aladin`（自建实现，**不是**内部真实阿拉丁；无 Langfuse、无线上用户数据）
- 分支 / Commit：`main` / `b121c4e`（`fix: 执行中发消息不再落库`）
- 语言与规模：Go 1.24 单模块（241 个 `.go`），React + TS 前端（51 个 `.ts/.tsx`），MySQL 8.0 + Redis 7，迁移 0001–0011
- 授权环境：本机 `deploy/docker-compose.yml` 起的 MySQL/Redis；本地临时目录与本地 bare 仓库
- 审计方法：`grep` 定位 → 沿调用链读实现与测试 → 交叉核对 wire-up（`cmd/server`、`internal/daemon/boot.go`）

---

## 0. 结论摘要（先看这五条）

1. **架构在 2026-11-20 被整体替换**：`f0d9c44 refactor!: 删除计划-步骤链路，Issue 改为一轮一次的长会话`。
   当前真实链路是 `Issue → 一轮执行(issue_runs) → WS 下发 → daemon + ACP → 上行事件 → Agent 用 CLI 自报状态`，
   手册里假设的 `Task→Plan→Step→Validate` 在迁移 0009 中已 `DROP TABLE`（`migrations/0009_issue_runs.up.sql:3-8`）。
   **旧文档（`docs/main-path-plan.md`、此前的 plan_runner 描述）描述的是删掉的上一个版本，不能当作现状。**
2. **平台不做任何验收**。`completed` 完全来自 Agent 自己调用 CLI 上报，`Task.ReportStatus` 只校验取值与「终态不可复活」
   （`internal/domain/task.go:117-132`），不看 commit、不看 PR、不看测试。这正是手册专项 C 的靶心。
3. **多处可靠性/观测组件「实现完整、单测齐全，但生产没接线」**：执行租约续约、未确认消息重投、背压、
   process span、workspace 指标、上下文装配、上下文版本同步、评测 Runner、小队派发器、规划会话下发。
   完整清单见 §4。这意味着「规格里有的保护」在运行中的 server 里默认不生效【代码证据】。
4. **主路径的 PR 交付在本次重构后断链**【代码证据 + **待运行验证**】：`tasks.plan_branch` 与
   `task_repositories.target_branch` 全仓库**没有任何写入方**，而 daemon 在 `params.Branch == ""` 时
   直接跳过 push 与 PR（`internal/daemon/turn_handler.go:477`）。
5. **评测资产比预期好**：`evals/swebench/` 已有一条真实跑通的 SWE-bench 实例（reward=1，`evals/README.md`），
   `internal/evaluation` 有 Report/Summarize 骨架（缺失数据不补零）。P1 的 Harness 不必从零写。

---

## 1. 当前真实调用链（逐段 `path:line`）

```
POST /workspaces/{wid}/issues
  └─ task.Service.Create                     internal/task/service.go:59
       ├─ normalizeAndValidate               internal/task/validate.go:17
       ├─ InsertTask / InsertTaskAgent /
       │  InsertTaskRepository(base_sha='', target_branch='')   internal/task/service.go:189-195
       └─ OnCreated 回调 → StartFromRequirement                cmd/server/main.go:136-142
            └─ issue.Service.StartTurn        internal/issue/service.go:36
                 ├─ 校验：任务未终态 / 无活跃 run / Agent 可调度 / 机器在线
                 ├─ EnsureSession             internal/issue/service.go:90
                 ├─ InsertIssueRun(status=dispatched)         :106-113
                 ├─ tasks.status → executing（乐观锁）         :115-122, :379-396
                 └─ Runtime.Dispatch          :124-139
                      └─ ws.Hub.Dispatch      internal/transport/ws/hub.go:88-152
                           ├─ params 组装（含 work_dir/repo_url/branch/system_prompt）
                           ├─ Envelope{Type=command, TraceID=idgen.New()}
                           └─ method = "start_issue_turn"
                                └─ daemon TurnHandler.Handle      internal/daemon/turn_handler.go:65
                                     └─ run()                     :102
                                          ├─ workspaceDir()       :508（<work_dir>/aladdin/<issue8>）
                                          ├─ git.Prepare + EnsureClean   internal/daemon/git.go:45
                                          ├─ acp.Provider.Start/Resume  :236
                                          ├─ Session.Prompt()     internal/transport/acp/provider.go:299
                                          │    ├─ 流式帧 → Sender.SendStream
                                          │    └─ 工具帧 → work_event     :160-190
                                          ├─ deliver()            :459（commit/push/PR）
                                          └─ SendEvent("turn_done")     :220-236
                                               └─ issue.EventHandler.Handle  internal/issue/events.go:21
                                                    └─ ApplyTurnDone     internal/issue/service.go:239-291
                                                         ├─ issue_runs → completed
                                                         ├─ SetIssueRunUsage（input/output tokens）
                                                         ├─ 会话游标 acp_session_id
                                                         └─ activities: turn.done / message.assistant

Agent 侧（与上面并行、不同通道）：
  aladdin openapi issue update-status --status completed
    └─ POST /api/v1/cli/issues/{id}/status   internal/task/cli.go:255
         └─ Task.ReportStatus（无任何验收）
```

**关键语义**：
- 一轮执行的终态（`issue_runs.status`）≠ 任务终态（`tasks.status`）。`turn_done` 只关这一轮，
  任务状态由 Agent 的 CLI 上报驱动（`internal/issue/service.go:236-238` 的注释明确写了这一点）。
- `TaskCompleted` 不是终态，可被再次打开；只有 `cancelled` / `failed` 是（`internal/domain/task.go:59-62`）。
- WS 信封里有 `TraceID`，但每帧都是 `idgen.New()` 新生成（`hub.go:128`），**没有跨进程 trace 传播**
  （`internal/observability.TraceID` 也无人调用）。

---

## 2. 数据来源核查（Langfuse / Token / 状态 / 上下文）

### 2.1 Langfuse / 通用 tracing

**【代码证据】不存在。** 全仓库无 `langfuse`、`opentelemetry`、`traceparent` 命中。

- `internal/observability/trace.go:23` 提供进程内 `StartSpan`，但**没有任何调用方**
  （仅 `observability.Log()` 在 `cmd/server/main.go:46`、`internal/daemon/boot.go:65` 被用于日志）。
- 结论：报告里只能写「自有最小埋点（结构化日志 + MySQL 事件流）」，**不可**声称有分布式 Trace。

### 2.2 Token / 用量

| 环节 | 证据 | 口径与缺口 |
|---|---|---|
| ACP 返回 | `internal/transport/acp/provider.go:329-337` | 读 `session/prompt` 结果的 `usage.inputTokens/outputTokens`；**缺失时结构体零值 = 0** |
| daemon 上报 | `internal/daemon/turn_handler.go:226-227` | 原样放进 `turn_done.input_tokens` |
| 服务端落库 | `internal/issue/service.go:258-262` → `SetIssueRunUsage` | `issue_runs.input_tokens BIGINT NOT NULL DEFAULT 0`（`migrations/0011_issue_run_usage.up.sql`） |
| 聚合展示 | `internal/stats/service.go:88-100`、`internal/stats/prometheus.go:63-68` | 按 `model` 汇总；`model` 是执行时 Agent 配置快照 |

**缺口【代码证据】**：缺失用量与真实 0 无法区分；无 `usage_source` / `usage_complete` 字段；
模型请求数、缓存命中、思考 token 均不可见。手册 §4.6 第 6 条、§9.3「缺失不能记 0」在实现上不满足。
`evals/README.md`「已知缺口」也自述「模型请求数不可见」。

### 2.3 任务 / 执行状态

- 状态源：`tasks.status`（枚举见 `internal/domain/task.go:31-40`）、`issue_runs.status`
  （`internal/domain/run.go:9-13`）、活动流 `activities`（kind + JSON payload）。
- 状态写入只有两处：平台 `setStatus`（`internal/issue/service.go:380-396`）与 Agent CLI
  `UpdateStatus`（`internal/task/cli.go:255-300`），两者都是「读 version → `UpdateTaskState`」的乐观锁。
- 指标口径：`internal/stats/service.go:329-346` 的 `SuccessRate` 实为**轮次正常结束率**
  （`completed` 轮次 / 已终态轮次），前端已改名「轮次正常结束率」（`web/src/pages/Dashboard.tsx:250`）；
  `Issues.CompletionRate` 才是 Agent 自报完成占比（`internal/stats/service.go:195`）。
  **两者都不能当任务成功率。**

### 2.4 上下文（平台可控边界）

- **当前真正生效的上下文机制只有两条**：
  1. ACP 长会话复用：`sessions.acp_session_id` 在 `turn_done` 时保存（`internal/issue/service.go:263-267`），
     下一轮 `Resume`（`internal/daemon/turn_handler.go:236-250`；`acp/provider.go:264-283`），
     适配器不支持时退回新会话。
  2. 提示词拼装：`system_prompt`（Agent 配置）+ 本轮 prompt + 平台上报说明
     （`internal/daemon/turn_handler.go:530-543`、`:587-615`）。
- **未生效的上下文设施**【代码证据】：
  - `agentctx.Assemble` / `agentctx.Fit`（`internal/agentctx/assembler.go:38`、`budget.go:62`）——
    只被自身单测调用，生产链路零引用。
  - `daemon.ContextSync`（`internal/daemon/contextsync.go:33`，含 `MergeAndPut` 冲突合并）——
    只在 `contextsync_test.go` 里构造，`boot.go` 未接线。
  - 上下文版本 HTTP API（`/api/v1/cli/contexts`，`cmd/server/main.go` `MountCLI`）——通道在，客户端不在。
- 平台对工具输出只能「看」，不能「改」：工具入参 `summarizeToolInput` 截断到 200–300 字符、
  原始 JSON ≤4KB（`turn_handler.go:383-436`），ACP 内部上下文完全不可见。

---

## 3. 重试 / 恢复 / 验证机制

### 3.1 已有的（生产生效）

| 机制 | 证据 | 行为 |
|---|---|---|
| 命令幂等 | `internal/daemon/idempotent.go` + `dispatcher.go:57-74` | 按 `command id` 落盘去重，重启仍生效 |
| 子进程回收 | `internal/daemon/reap.go`、`janitor.go` | 独立进程组托管 ACP 子进程，重启清理孤儿 |
| 断连清理 | `cmd/server/main.go:177-184` → `issue.Service.FailStaleRuns` | 机器重连时把该机未收尾的 run 置 `failed`（`error_class=runtime`, `uncertain=true`），**不改任务状态** |
| 派发失败回滚 | `internal/issue/service.go:140-147` | run → failed，任务状态回滚到派发前 |
| 单轮超时 | `acp/provider.go:306-317`（默认 30 min，evalMode 4 h） | 超时 → `ErrPromptTimeout` + `ErrUnknownResult`，事件带 `uncertain=true` |
| 执行互斥 | `issue/service.go:52-56`、`:188-192` | 同一 Issue 同时只允许一轮在跑；执行中拒绝新消息 |
| 任务锁（小队） | `internal/squadrunner/tasklock.go` | Redis `lock:task:{id}` |

### 3.2 实现了但**没接线**的（若声称「有防护」会失真）

| 能力 | 实现 | 未接线的证据 | 影响 |
|---|---|---|---|
| 执行租约续约 | `internal/store/redis/lease.go`；`hub.go:162-178` | `hub.Lease` 字段在 `cmd/server/main.go` 从未赋值 → `handleHeartbeat` 直接 return | 心跳里的 `active_executions` 被静默忽略，「租约过期」不存在 |
| 未确认消息重投 | `internal/transport/ws/reliable.go:13`、`internal/store/redis/unacked.go` | 全仓库无 `ResendLoop`/`NewUnackedStore` 调用（除测试） | 命令/事件丢失后不会重投，只能靠上层超时 |
| 背压 | `internal/transport/ws/backpressure.go:72` | 无构造调用 | 慢消费者无暂停机制 |
| 迟到结果围栏 | `attempt` 模型 + `execution_id` 唯一键（`migrations/0009:22`） | `MarkRunning` 用 `status='dispatched'` 条件更新（`store/mysql/issue_run.go:135-144`） | 见 §3.3 |
| 进程内 span / 指标 | `internal/observability/trace.go:23`、`metrics.go:26` | 无调用方 | 只能靠日志与 DB 回溯 |
| 上下文装配/预算 | `internal/agentctx/*` | 无调用方 | 手册候选 A 的「平台可控上下文层」目前是死代码 |
| 评测 Runner | `internal/evaluation/runner.go` | 无调用方 | 真实 harness 在 `evals/swebench/`（脚本层） |
| 小队派发 | `internal/squadrunner/squad_runner.go:31-41, 155-158` | `Dispatcher` 生产为 nil → `dispatch()` 直接 return nil | 见 §3.4 |
| 规划会话 | `internal/session/service.go:43`、`hub.go:210` | `DispatchPlanning` 无调用方；daemon 只认 `start_issue_turn`/`stop_session`（`turn_handler.go:67-78`） | 规划链路整条不可用 |

### 3.3 迟到结果与重复副作用

**【代码证据】** 有天然围栏也有弱点：
- 围栏：`issue_runs.execution_id` 唯一（`migrations/0009:22`），run 状态迁移用条件更新
  （`FinishIssueRun` 要求 from ∈ {dispatched, running}），所以**同一 run 的重复 `turn_done` 只会生效一次**。
- 弱点 1：`hub.execMachines` 只写不删（`hub.go:149` 写入，无删除点）→ 长时间运行后内存单调增长；
  `Cancel` 依赖这张表，历史 execution 的取消请求可能被送到一台早已换过任务的机器（随后被 daemon 以
  `not_running` 拒绝，但请求确实发了）。
- 弱点 2：**外部副作用没有幂等键**。`deliver()` 先 `CommitAll` 再 push/PR；
  如果进程在 commit 之后、push 之前被杀，重试会新建一轮、重新 clone（工作区 `<work_dir>/aladdin/<id8>` 已存在，
  `Prepare` 会复用并 `EnsureClean`），上一轮的本地 commit 仍在但从未 push——
  「已发生但未记录」的副作用没有任何对账。
- 弱点 3：迟到结果只被 `execution_id` 挡住，**没有 epoch/代次参与**：`RunningExecutions.Start` 恒传 epoch=1
  （`turn_handler.go:106`），心跳里的 `lease_epoch` 永远没有意义。

### 3.4 小队与规划链路

- 小队任务创建时只冻结快照（`internal/squad/snapshot.go:27-64`），**从不创建 work item**：
  `mysqlstore.InsertWorkItem` 无生产调用方。`squadrunner.Loop` 每秒跑一次（`cmd/server/main.go:246`），
  但 `ListWorkItems` 永远为空 → 小队模式实际是**空转**【代码证据】。
- 即使有 work item，`Dispatcher == nil` 时 `dispatch()` 返回 nil（`squad_runner.go:155-158`），
  工作项会停在 `running`，而全仓库没有任何超时/对账把它改回 `blocked`（`WorkItemRunning` 的唯二写入点见 §4）。
- 规划会话（`start_planning_session`）daemon 侧未实现，`hub` 只会拿到 `unsupported_method` 拒绝。

### 3.5 验证（Validate）层

**【代码证据】不存在独立验收。** 唯一「验证」是 daemon 对 ACP 的 `stopReason` 判断：

```go
switch result.StopReason {
case "end_turn": ... turn_done
default:        ... turn_fail(error_class=verification)
}
```
（`internal/daemon/turn_handler.go:207-238`）

`end_turn` 只说明**模型这一轮说完了**，不代表改了文件、跑过测试、建了 PR。证据：
- `deliver()` 在没有改动时返回 `("", "")` 且不报错（`turn_handler.go:470-475`）；
- 平台随后仍然接受 Agent 上报的 `completed`（`domain/task.go:117-131`）。

外部评测（`evals/`）刻意绕开这一点：判分在干净容器里跑官方 `verify.py`，不用 Agent 自报
（`evals/README.md`「保持不变的四条」第 4 条）。**平台内部没有任何同等机制。**

---

## 4. 已有测试与实验资产

| 资产 | 位置 | 说明 |
|---|---|---|
| 单元测试 | `internal/**/*_test.go` | 251 个 `Test*`；覆盖状态机、hub、guard、agentctx、evaluation 等 |
| 集成测试 | `-tags=integration`，`Makefile:49` | 真 MySQL/Redis；CI 里 `-p 1` 串行 |
| 端到端（stub runtime） | `tests/e2e/ordinary_test.go` | `TestIssueTurnLifecycle`、`TestIssueTurnFailKeepsStatus`、`TestIssueTurnDispatchFailureRollsBack`、`TestFailStaleRunsUnblocksIssue`、`TestEvalModePausesAfterTurn`、`TestStreamMarksRunRunning` |
| 端到端（小队） | `tests/e2e/squad_test.go` | 只覆盖状态机 |
| 性能基线 | `tests/perf/baseline_test.go` | 并发派发延迟，`PERF_N` 可调 |
| UI E2E | `web/e2e/*.spec.ts` | Playwright：真 server + 真 daemon + fakeagent + 本地 bare 仓库 + `local://` PR |
| SWE-bench 接入 | `evals/swebench/` + `evals/jobs/sympy-20590-local/` | 已跑通 1 个实例，reward=1；含 patch/事件/日志/判分证据 |
| 夹具 | `tests/fixtures/acp/fakeagent/main.go` | 可脚本化 ACP 行为（含 `write_file` 模式），**零模型成本** |

**可复用的隔离手段（对三个候选实验都关键）**：
- `fakeruntime`（`internal/testkit/fakeruntime`）与 e2e 的 `recordingRuntime`：完全确定性。
- fakeagent：把 ACP 行为变成可控状态机（成功、失败、超时、不写文件、写文件）。
- `ALADDIN_PR_PROVIDER=local`（`internal/daemon/pr.go:128`）：PR 变成 `local://pull/<branch>`，不触网。
- 本地 bare 仓库 + 临时 work dir：无真实副作用。

---

## 5. 未确认项

1. **主路径交付断链的影响面**：静态阅读显示 `plan_branch` 无写入方，但没跑端到端确认。
   需要一次真实（或 fakeagent）执行看 `turn_done.branch == ""` 且无 PR。
2. **`turn_done` 之后任务长期停在 `executing` 的业务后果**：Agent 忘记上报时，看板「进行中」会一直挂住；
   是否有外部兜底（定时任务/人工）不在本仓库。**未确认**。
3. **Redis 里是否残留旧版（Plan/Step）键**：`internal/store/redis/keys.go` 仍保留 `plan:`、`lease:attempt:`、
   `recovery:dirty` 等键定义，但对应写入方已随 0009 迁移删除。是否有运维侧消费者，**未确认**。
4. **CI 是否真的跑 e2e/perf**：`.github/workflows/ci.yml` 的 job 列表未逐条核对（本次只确认 Makefile 目标存在）。
5. **`sessions.acp_session_id` 复用失败率**：适配器不支持 resume 时会静默回退新会话
   （`turn_handler.go:236-250`），没有任何指标记录回退次数。**未确认**其发生频率。

---

## 6. 候选实验（≤3 个）与隔离可行性

打分沿用手册 §2.1 的五维（1–5）。**推荐顺序：EX-1 → EX-2 → EX-3。**

### EX-1：执行权与迟到结果——「保护存在」还是「保护生效」？（推荐首选）

- **研究问题**：Worker 断连、重连、重复上报时，平台会不会接受过期结果、重复推进状态、或让 Issue 永久卡住？
- **现状证据**：§3.2 的未接线清单（租约续约、重投、epoch 恒为 1、`execMachines` 不清理）。
- **假设**：
  - H0：现有 `execution_id` 唯一键 + 条件更新已足够，故障注入下违规数为 0。
  - H1：`execution_id` 只能挡住同一 run 的重复，挡不住「重连后旧 run 的迟到帧与新 run 并发」，
    且未接线的心跳续约使「断连后继续执行」不可检测。
- **用例（对应手册 R1–R4，全部确定性）**：
  - R1 daemon 断开 WS 但进程仍在跑（fakeagent 阻塞）→ 重连触发 `FailStaleRuns`；断言旧 run 不再被接受。
  - R2 run A 被标记 failed 后，其 `turn_done` 迟到 → 断言不会把 run 改回 completed。
  - R3 `turn_done` 写了 DB 但客户端没收到 ack（模拟事件丢失）→ 断言状态收敛且不重复计数。
  - R4 同一 Issue 收到两个 `turn_done`（不同 execution_id）→ 断言第二轮被拒（`ActiveIssueRun` 语义）。
- **隔离实现**：真 MySQL/Redis（docker compose）+ `recordingRuntime`/`fakeserver` + fakeagent。**零模型成本**。
- **指标**：违规状态更新数（应 0）、过期结果接受数（应 0）、卡在 `executing` 的 Issue 数、恢复时长。
- **可行性：5/5。** 完全确定、可重复、改动面小（`hub` / `issue` / `daemon`）。
- **注意**：Fencing 只约束「服务端接受结果」，不阻止旧 Worker 继续写文件/推分支 —— 结论里必须写清这条边界。

### EX-2：`completed` 与「真交付」的绑定（对应手册候选 C）

- **研究问题**：Agent 上报 `done` 但实际没有改动 / 没有 PR / 测试失败时，平台是否会接受终态？
- **现状证据**：§3.5；`Task.ReportStatus` 无验收；`deliver()` 无改动时不报错。
- **用例（fakeagent 可完全构造）**：
  1. 写 0 个文件 + 上报 completed；
  2. 写文件但 `deliver` 因 `Branch=""` 跳过 push（**顺带验证 §0 第 4 条的断链**）；
  3. 写了文件但测试命令失败 + 上报 completed；
  4. 上报 blocked / failed（对照，应保持现状）。
- **优化候选（最小 diff）**：`UpdateStatus` 接受 `completed` 时要求一条可核查证据
  （本轮 `commit_sha` 非空，或 `changed_files > 0`，或外部 verifier 回执），否则降级为
  `paused` 并记录 `completion_rejected` 事件；或引入 `reported_completed ≠ verified_completed` 两列。
- **指标**：错误终态接受数、误拒数、每类用例的最终状态；护栏：真完成用例不能被拒。
- **可行性：5/5**（确定性）；但**产品决策**需要你拍板：拒绝的语义是「拒绝上报」还是「记录但不认」。
- **风险**：改动 `tasks.status` 语义会影响前端与 CLI，需要同步改文案与测试。

### EX-3：平台可控上下文对结果的真实影响（对应手册候选 A，成本最高）

- **研究问题**：在平台能控制的两处（`system_prompt` 拼接、`platformInstructions` 注入）变化时，
  任务成功率 / 轮次耗时 / 真实 token 是否可测地变化？
- **现状证据**：§2.4；evalMode 已经刻意不注入平台提示词（`turn_handler.go:589-591`），
  这说明「注入会影响结果」已被当作前提，但**没有量化**。
- **设计**：用 `evals/swebench/run-instance.sh` 固定镜像 + base commit + 模型；
  对照组 A（现网默认，含上报说明）、B（evalMode，不注入）、C（结构化约束注入）。
  同一批 5–10 个任务成对比较。
- **指标**：官方 reward（唯一可信判分）、轮次正常结束率、耗时、可采集 token（注明覆盖率）。
- **可行性：3/5**。需要真实模型调用（单实例约 900 s，见 `evals/README.md`）、QEMU 下更慢、非确定性；
  样本小，只能给方向性结论。
- **必须先解决的前置**：token 缺失记 0 的问题（§2.2），否则任何「省 token」结论都不成立。

**明确不建议现在做**：手册候选 D（Git 合入基线一致性）。当前 `deliver()` 连 push 都跳过，
谈不上「合入验证」；应先修复 §0 第 4 条的断链，再考虑 D。

---

## 7. 与实验无关、但审计中确认的缺陷（需单独决策）

| # | 缺陷 | 证据 | 严重度 | 备注 |
|---|---|---|---|---|
| D-1 | 主路径 push/PR 断链 | `plan_branch`/`target_branch` 无写入方；`turn_handler.go:477` | 高（对外宣称的能力失效） | 与 commit `f0d9c44` 同源 |
| D-2 | 小队模式空转 | `InsertWorkItem` 无生产调用方；`Dispatcher=nil` | 中 | 功能未完成，与实验无关 |
| D-3 | 规划会话不可用 | daemon 未实现 `start_planning_session` | 中 | 已被 Issue 长会话取代，可考虑删代码 |
| D-4 | 用量缺失记为 0 | `issue_runs.input_tokens DEFAULT 0` | 中 | 直接影响 EX-3 与看板可信度 |
| D-5 | `hub.execMachines` 只增不删 | `hub.go:149` | 低 | 长时间运行内存增长 |
| D-6 | `epoch` 恒为 1 | `turn_handler.go:106` | 低（在 EX-1 范围内） | 围栏语义形同虚设 |
| D-7 | 过期回执仍写副作用 | `internal/issue/service.go:246`、`:301`（`FinishIssueRun` 的 `ok` 被丢弃） | **高** | **EX-001 已复现并修复**，见 `experiment-001.md` |

> 按手册 §9.3 的纪律：以上都是**代码证据**，其中 D-1 需一次运行复现后才可写进对外的报告。

---

## 8. 需要你批准的事项

### 8.1 数据访问

| 项目 | 默认做法 | 需要批准吗 |
|---|---|---|
| 本机 docker compose 的 MySQL/Redis（`aladdin_test` 库） | 只写测试夹具数据，跑完清理 | 否（既有 `make test-integration` 同款） |
| `evals/jobs/sympy-20590-local/` 里的真实执行日志 | 只在报告中引用指标与文件清单，不贴完整日志 | **是**（含真实模型输出） |
| 真实 GitHub 仓库 `roarzx/aladdin-demo` | **默认不动**；如需真实 PR 再单独申请 | **是** |
| 真实 Claude Code / 模型调用 | EX-1/EX-2 不需要；EX-3 需要 | **是**（成本见下） |

### 8.2 计划执行的命令（全部在本机）

```bash
# 基础设施（Docker Desktop 需先启动）
make up && make migrate

# 单元 + 集成（集成需 -p 1，避免跨包互相干扰）
make test
make test-integration

# 端到端（stub runtime，无模型）
go test -tags=e2e ./tests/e2e/... -count=1

# UI E2E（真 server + 真 daemon + fakeagent + 本地 bare 仓库）
make test-ui-e2e

# EX-1 / EX-2 的故障注入（新增测试，不触网）
go test -tags=integration ./internal/issue/... -run TestLateResult -count=1
```

**不会执行**：任何 `git push`、真实 `gh pr create`、生产数据库写入、外部数据上传。

### 8.3 成本估算

| 项目 | 估算 | 备注 |
|---|---|---|
| EX-1 / EX-2 | 0 模型成本 | fakeagent + 本地容器；主要成本是人力与 CI 机时 |
| EX-3（如做） | 每实例 1 次真实跑 ≈ 15 min 墙钟（QEMU 下）、模型 token 未计量 | 5–10 实例 × 2–3 组 ≈ 10–30 次跑；建议先跑 1 个实例校准 |
| SWE-bench 镜像构建 | 首次 10–20 min（`evals/swebench/Dockerfile`） | 缓存后复用 |

---

## 9. 建议的下一步

1. 你确认 EX-1 为第一项（或改选 EX-2）。
2. 我先写 `docs/experiments/questions.md`（假设登记表）与 `docs/experiments/README.md`（索引 + 运行方法）。
3. 再按手册 §9.1 写 `experiment-001.md` 预注册（单一变量、固定项、停止条件），**先跑 Baseline 不改代码**。
4. D-1/D-2 作为独立 issue 处理，不混进实验结论。

---

## 附录 A：复核用命令（只读）

```bash
# 主链路
grep -n 'func (s \*Service) StartTurn\|Runtime.Dispatch' internal/issue/service.go
grep -n 'func (h \*Hub) Dispatch' -A40 internal/transport/ws/hub.go
grep -n 'func (h \*TurnHandler) run\|deliver' internal/daemon/turn_handler.go

# 未接线清单（应无输出或只有测试）
grep -rn 'ResendLoop\|NewUnackedStore\|NewLeaseStore' --include='*.go' . | grep -v _test
grep -rn 'agentctx.Assemble\|observability.StartSpan\|evaluation.Runner' --include='*.go' . | grep -v _test

# 分支断链
grep -rn 'plan_branch\|target_branch' --include='*.go' . | grep -v _test

# 小队空转
grep -rn 'InsertWorkItem' --include='*.go' . | grep -v _test
```
