# 阿拉丁执行可靠性基线（EX-001）

- 数据日期：2026-11-24
- 运行环境：本机 macOS (arm64)，Docker `deploy/docker-compose.yml`（MySQL 8.0 `aladdin_test` + Redis 7）
- Dataset：`tests/e2e/recovery_test.go`（R1–R5），版本随 commit 固定
- 阿拉丁 Commit：`b121c4eb585cfd2948c530afe4f6f9877137f10a`（Baseline）；
  Treatment 为该 commit 上的工作区改动（`internal/issue/service.go`）
- 模型：无（stub runtime，零模型调用）
- 配置 Hash：n/a（无模型配置参与）
- 有效 Case 数 / Run 数：5 / 5（每 Case 独立夹具，单次运行）

## 指标

| 指标 | Baseline | Treatment |
|---|---:|---:|
| 过期结果被接受产生的副作用数（主指标） | **7** | **0** |
| 违规状态更新数（run 从终态被改写） | 0 | 0 |
| 用量被过期/重复回执改写的 Case 数 | 2（R2、R3） | 0 |
| 无法继续执行的 Issue 数（卡死） | 0 | 0 |
| 全量运行耗时 | ~1.4 s | ~0.7 s |

## 失败分类

| Case | 类别 | 具体表现 |
|---|---|---|
| R2 | 过期结果接受 | `turn_done` 在 run=failed 后仍写 `message.assistant` / `turn.done` / usage |
| R3 | 重复副作用 | 同一 run 的第二次 `turn_done` 覆盖用量并重复活动 |
| R4 | 状态-事件不一致 | run=completed 后出现 `turn.failed` 活动 |
| R5 | 跨轮污染 | 上一轮迟到回执写入新一轮的活动流 |

## 结论

- **H1 成立、H0 被推翻**：`execution_id` + 条件更新能保护 `issue_runs.status`，
  但**不能**保护由回执触发的副作用（活动流、用量、ACP 会话游标）。
- 修复后主指标为 0，且合法路径零回归（单测、全量 e2e、集成测试全绿）。

## 未覆盖的任务类型 / 边界

- **未验证**外部副作用（文件写入、push、PR）的重复执行——当前 `Branch` 为空，push 不触发（见 `inventory.md` D-1）。
- **未覆盖**心跳租约与 epoch：`hub.Lease` 未接线，`epoch` 恒为 1。
- **未覆盖**真实 ACP 适配器的重放行为：实验在服务端事件层注入，不依赖模型。
- 样本量：每 Case 单次；由于全流程确定性（无模型、无网络），未做重复运行。

## 限制

- 测试夹具在共享 `aladdin_test` 库上运行，清理按 workspace 维度；与并行集成测试同时跑需要 `-p 1`。
- 这是**实验环境**结果，不代表任何线上改进。
