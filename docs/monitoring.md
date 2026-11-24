# 监控与看板（P0）

监控数据全部来自 MySQL 里已有的事实表，不引入外部依赖：

- `issue_runs`：每轮执行的状态、起止时间、token 用量、模型快照、Agent 归属
- `activities`：Issue 状态变更、工具调用（含耗时与结果）
- `tasks` / `machines`：当前 Issue 与机器状态

## 三个视图

对应三个不同的问题，不要混成一个页面：

| 视图 | 页面 | 回答什么 |
|---|---|---|
| 平台总览 | `/` | 任务量、上报完成率、失败率、P95 轮时长、Worker 可用性、每日趋势 |
| 执行分析 | `/monitor/execution` | 哪个 Agent 跑得多/快/稳、工具耗时、失败分类、token 归属 |
| 单任务详情 | `/issues/{id}` →「记录」 | 这一轮执行了什么、每一步多久、失败在哪 |

单任务详情顶部有「排队 / 执行 / 收尾」耗时条与最慢工具；平台总览支持按 Issue ID 直接跳转。

## 端点

| 端点 | 鉴权 | 用途 |
|---|---|---|
| `GET /metrics` | 无（只读聚合） | Prometheus 文本格式 |
| `GET /stats/overview?days=7\|30` | Bearer | 两个页面的数据源 |

每次请求按窗口查库聚合，结果缓存 15 秒；没有常驻内存计数器，重启不丢数，
也不会因为重复事件重复计数。

## 指标口径

| 指标 | 定义 | 说明 |
|---|---|---|
| `aladdin_issues{status}` | 当前各状态 Issue 数 | 全量，不分窗口 |
| 每日创建 / 完成 | `tasks.created_at` / `issue.status.to=completed` 事件 | 完成按事件算，一个 Issue 可以完成后再打开 |
| `aladdin_runs_total{status}` | 窗口内轮次数 | — |
| Issue 完成率 | 当前 `tasks.status=completed` / 全部 Issue | **Agent 上报完成**，与上面的轮次口径不同 |
| `aladdin_run_success_ratio` | `completed / (completed+failed+cancelled)` | **轮次正常结束率**：只算终态；`completed` 指这一轮正常收尾（turn_done），不是 Agent 上报完成 |
| `aladdin_run_error_ratio` | `failed / 终态轮次` | 与完成率的分母一致 |
| `aladdin_runs_retried_total` | 同一 Issue 第 2 轮及以后的执行数 | **含用户追加消息**，不是纯重试 |
| `aladdin_run_duration_seconds{status}` | 单轮耗时直方图 | 仅终态轮次；`updated_at - created_at` |
| `aladdin_run_failures_total{error_class}` | 失败分类 | network/runtime/tool/verification/permission/dispatch |
| `aladdin_tool_calls_total{tool,status}` | 工具调用次数 | 取 Top 8，其余归「其他」 |
| `aladdin_tool_duration_seconds{tool,quantile}` | 工具耗时分位 | 只保留 0.5 / 0.95 |
| `aladdin_tokens_total{direction,model}` | input/output token | 每轮 Prompt 合计，不是每次模型调用 |
| `aladdin_agent_runs_total{agent,runtime,status}` | Agent 维度轮次 | 标签是「名字/ID 前 8 位」 |
| `aladdin_agent_tokens_total{agent,direction}` | Agent 维度 token | — |
| `aladdin_machines{status}` / `aladdin_machines_availability` | 机器在线数与在线率 | — |
| 本轮耗时分解 | 排队 / 执行 / 收尾 | `run.created_at → turn.started → turn.done → run.updated_at` |

## 明确不做

- **不做高基数指标**：`task_id` / `run_id` / `execution_id` 只出现在页面与事件里，不进标签。
- **不上报内容**：prompt、文件内容、工具入参与输出不进监控；`/metrics` 只有聚合数字。
- **不把「上报完成」当业务成功**：Agent 自己上报 completed 只代表它认为做完了，
  真正的质量由 evals 判定。看板同时展示「轮次正常结束率」（执行健康）和
  「Agent 上报完成」（业务状态），两者不要混用。
- **不做成本估算（P0）**：只记录 token，单价表留到需要时再加。
- **不伪造上下文压缩数据**：ACP 事件里没有压缩行为，页面明确标注「尚未采集」。

## 已知限制

1. `activities` 不随 Issue 删除级联，聚合查询一律 `JOIN tasks` 排除孤儿行。
2. 模型归因取执行时 `agents.model` 的快照（写入 `issue_runs.model`）；未配置时记为 `unknown`。
3. 日分组按 UTC 切日；前端按本地时区显示时间，但不改日期桶。
4. `/metrics` 不鉴权，只应在内网或本机暴露；公网部署前需加 token。
5. **历史数据**：迁移前完成的轮次没有 token（记 0）；2026-11-23 及之前的记录里
   有一批「未知」工具调用，是 `session/load` 重放的旧调用被重复上报，已在
   daemon 侧修复（无标题的重放直接丢弃），旧数据保留。

## 后续（P1/P2）

- P1：daemon 侧计量（ACP 会话建立失败率、prompt 时延、上下文压缩）、卡住轮次检测、站内告警
- P2：按需接 Prometheus + Grafana 或 OTel/Langfuse，且只传元数据与摘要
