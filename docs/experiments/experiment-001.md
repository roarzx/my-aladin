# 实验 EX-001：执行权与迟到结果（预注册）

> 按手册 §9.1 在**运行任何实验之前**登记。登记后只允许改「结果」一节，其余变更必须写修订说明。
> 状态：**已预注册，Baseline 未跑**（2026-11-24）

## 1. 研究问题

Worker 断连重连、重复上报、迟到结果发生时，平台是否接受过期结果、重复推进状态或让 Issue 永久卡住？

## 2. 现状证据（代码）

| 证据 | 位置 |
|---|---|
| 回执处理不检查条件更新是否命中 | `internal/issue/service.go:246-250`、`:301-305`（`FinishIssueRun` 的 `ok` 被 `_` 丢弃） |
| 条件更新的「围栏」本身 | `internal/store/mysql/issue_run.go:94-122` |
| 断连清理 | `internal/issue/service.go:317-341`，由 `cmd/server/main.go:177-184` 在重连时调用 |
| 执行互斥 | `internal/issue/service.go:52-56` |
| 轮次终态定义 | `internal/domain/run.go:9-13` |

## 3. 假设

- **H0**：现有约束已足够；迟到/重复回执对状态、活动流、用量均无副作用。
- **H1**：状态列被保护住了，但**过期回执仍会写活动流与用量**（见 questions.md）。

## 4. 数据集与 Case

固定夹具：真 MySQL/Redis（`aladdin_test`），`tests/e2e` 的 `setupIssue` + `recordingRuntime`，
fakeagent 不参与（本实验在服务端事件层注入，不花模型成本）。

| Case | 注入 | 核心断言 |
|---|---|---|
| R1 断连重连 | run 处于 running → 调用 `FailStaleRuns(machine)` | run→failed(`error_class=runtime`)；活动含 `turn.failed{uncertain:true}`；随后可开新一轮 |
| R2 迟到 `turn_done` | run 已 failed → 投递 `turn_done` | run 仍 failed；**活动流与用量零增量** |
| R3 重复 `turn_done` | 同一 run 连续两次 `turn_done` | 只产生 1 条 `message.assistant`、1 条 `turn.done`、用量不翻倍 |
| R4 迟到 `turn_fail` | run 已 completed → 投递 `turn_fail` | run 仍 completed；不新增 `turn.failed` |
| R5 旧 run 与新一轮并发 | run1 failed → run2 完成 → run1 迟到 `turn_done` | run1/run2 状态与用量互不覆盖，Issue 不出现两条「进行中」 |

## 5. Baseline / Treatment

- **Baseline**：commit `b121c4e`，不改任何生产代码，只新增测试 `tests/e2e/recovery_test.go`。
- **Treatment**：仅在 Baseline 出问题后提出，候选是「回执处理以 `FinishIssueRun` 的 `ok` 为准，
  未命中直接返回，不做任何副作用写入」（最小 diff）。
- **唯一主要变量**：回执是否被判定为「本 run 的有效回执」。

## 6. 固定变量

- 数据库：本机 Docker `aladdin_test`（迁移 0001–0011），Redis 6 号库不参与
- Go：1.24.5；Aladdin commit `b121c4e`
- 模型 / 提示词：**不涉及**（fake）
- 运行方式：`go test -tags=e2e ./tests/e2e/... -run 'TestRecovery' -count=1`
- 每个 Case 独立夹具，跑完清理

## 7. 指标

**主指标**：过期结果被接受后产生的副作用数（活动条数 + 用量增量），期望 0。

**护栏指标**：
- 违规状态更新数（run 从终态被改写），期望 0
- 卡在 `executing` 且无法继续的 Issue 数，期望 0
- R1 后重新开轮的派发次数，期望 1

## 8. 成功 / 停止条件

- **成功**：Baseline 全绿 → 输出「现有保护覆盖范围 + 未覆盖边界」的负结果报告，不强行改代码。
- **发现缺陷**：任一 Case 断言失败 → 先缩成最小复现，再提最小修复，并补回归测试。
- **停止**：若 3 次运行结果不一致（非确定性），停止并把该 Case 标为「不可测」。

## 9. 成本预算

零模型调用；Docker 本地；单次全量运行 < 10 s。不设金额预算，设 2 小时人力上限。

## 10. 执行命令与结果文件

```bash
make up && make migrate
make test-e2e
go test -tags=e2e ./tests/e2e/... -run 'TestRecovery' -count=1 -v
```

结果写入：
- `docs/experiments/evidence/ex-001/baseline.json`（逐 Case 断言结果）
- `docs/experiments/reports/baseline.md`

## 11. 结果

**Baseline**（`b121c4e`）：1 绿 4 红，H1 成立。
**Treatment**：`ApplyTurnDone` / `ApplyTurnFail` 以 `FinishIssueRun` 的返回值作为围栏
（`internal/issue/service.go:245-257`、`:301-311`），未命中直接返回，不写任何副作用。

| Case | Baseline | Treatment | 备注 |
|---|---|---|---|
| R1 断连重连 | PASS | PASS | 原本就正确 |
| R2 迟到 `turn_done` | **FAIL** | PASS | 迟到回执写了 1 条 `message.assistant`、1 条 `turn.done`、100/50 用量 |
| R3 重复 `turn_done` | **FAIL** | PASS | 活动重复 2 条；用量被 10/5 覆盖成 100/50 |
| R4 迟到 `turn_fail` | **FAIL** | PASS | 活动流出现与终态矛盾的失败记录 |
| R5 跨轮迟到 | **FAIL** | PASS | 第二轮用量未受影响，但活动流被第一轮污染 |

**汇总**：Baseline 违规 7 处（其中 2 例使用量被改写）；Treatment 0 处。

**副作用 / 负面结果**：无。修复只收紧「过期回执不产生副作用」，不改变合法路径；
`turn_done` 的合法回执仍按原样写用量、会话游标与活动。

**边界（必须写进对外结论）**：
- 本实验只验证**服务端接受结果**的围栏，不阻止旧 Worker 继续执行外部写入（文件、push）。
- `hub.Lease` 未接线、`epoch` 恒为 1 的问题**没有**被本次修复覆盖，仍属未防护边界。
- 用量「缺失记 0」的问题未改（D-4）。

**证据**：
- Baseline 日志：`docs/experiments/evidence/ex-001/baseline.log`
- Baseline 结构化结果：`docs/experiments/evidence/ex-001/baseline.json`
- Treatment 日志：`docs/experiments/evidence/ex-001/treatment.log`
- 回归：`go test ./...`、`make test-e2e`、`make test-integration` 全绿
