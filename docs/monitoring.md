# 监控与看板（P0）

监控数据全部来自 MySQL 里已有的事实表，不引入外部依赖：

- `issue_runs`：每轮执行的状态、起止时间、token 用量、模型快照
- `activities`：Issue 状态变更、工具调用（含耗时与结果）
- `tasks` / `machines`：当前 Issue 与机器状态

## 端点

| 端点 | 鉴权 | 用途 |
|---|---|---|
| `GET /metrics` | 无（只读聚合） | Prometheus 文本格式 |
| `GET /stats/overview?days=7\|30` | Bearer | 前端看板 |

`/stats/overview` 每次请求按窗口查库聚合，结果缓存 15 秒；没有常驻内存计数器，
重启不丢数，也不会因为重复事件重复计数。

## 指标口径

口径先定死，避免看板与指标打架：

| 指标 | 定义 | 说明 |
|---|---|---|
| `aladdin_issues{status}` | 当前各状态 Issue 数 | 全量，不分窗口 |
| 每日创建 / 完成 | `tasks.created_at` / `issue.status.to=completed` 事件 | 完成按事件算，一个 Issue 可以完成后再打开 |
| `aladdin_runs_total{status}` | 窗口内轮次数 | — |
| 成功率 | `completed / (completed+failed+cancelled)` | 只算终态，运行中单独展示 |
| `aladdin_run_duration_seconds{status}` | 单轮耗时直方图 | 仅终态轮次；`updated_at - created_at` |
| `aladdin_run_failures_total{error_class}` | 失败分类 | network/runtime/tool/verification/permission/dispatch |
| `aladdin_tool_calls_total{tool,status}` | 工具调用次数 | 取 Top 8，其余归「其他」 |
| `aladdin_tokens_total{direction,model}` | input/output token | 每轮 Prompt 合计，不是每次模型调用 |
| `aladdin_machines{status}` | 机器在线数 | — |
| 本轮耗时分解 | 排队 / 执行 / 收尾 | 记录页顶部：`run.created_at → turn.started → turn.done → run.updated_at` |

失败分类与「本轮耗时分解」直接支撑排障：队列慢就去查派发，执行慢再去看工具事件。

## 明确不做

- **不做高基数指标**：`task_id` / `run_id` / `execution_id` 只出现在页面与事件里，
  不进 metrics 标签。
- **不上报内容**：prompt、文件内容、工具入参与工具输出不进监控；`/metrics`
  只有聚合数字。
- **不把「上报完成」当业务成功**：Agent 自己上报 completed 只代表它认为做完了，
  真正的质量由 evals 判定。看板文案写作「上报完成率」。
- **不做成本估算（P0）**：只记录 token，单价表留到需要时再加。

## 已知限制

1. `activities` 不随 Issue 删除级联，聚合查询一律 `JOIN tasks` 排除孤儿行。
2. 模型归因取执行时 `agents.model` 的快照（写入 `issue_runs.model`）；Agent 未
   配置模型时记为 `unknown`。
3. 日分组按 UTC 切日；前端按本地时区显示时间，但不改日期桶。
4. `/metrics` 不鉴权，只应在内网或本机暴露；公网部署前需加 token。

## 后续（P1/P2）

- P1：daemon 侧计量（ACP 会话建立失败率、prompt 时延）、卡住轮次检测、站内告警
- P2：按需接 Prometheus + Grafana 或 OTel/Langfuse，且只传元数据与摘要
