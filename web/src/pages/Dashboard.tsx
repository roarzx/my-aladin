import { useCallback, useEffect, useMemo, useState } from 'react'

import { fetchIssues, type Task, type TaskStatus } from '../api/issues'
import { fetchStats, type DayCount, type DayTokens, type NamedCount, type StatsOverview } from '../api/stats'
import AppShell from '../components/AppShell'
import { EmptyState, MetricCard, StateDot, StateTag, type TagTone } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-issues.css'

const STATUS_META: Record<TaskStatus, { label: string; tone: TagTone }> = {
  pending: { label: '待开始', tone: 'pending' },
  executing: { label: '执行中', tone: 'running' },
  paused: { label: '已暂停', tone: 'paused' },
  completed: { label: '已完成', tone: 'done' },
  cancelled: { label: '已取消', tone: 'cancelled' },
  failed: { label: '已失败', tone: 'failed' },
}

/** 「待我处理」：需要人介入的状态（失败重试、Agent 阻塞）。 */
const ATTENTION_STATUSES: TaskStatus[] = ['failed', 'paused']

/** 失败分类翻译；后端 error_class 是英文枚举。 */
const FAILURE_LABELS: Record<string, string> = {
  network: '网络 / 超时',
  runtime: '运行环境',
  tool: '工具调用',
  verification: '验收',
  permission: '权限',
  dispatch: '派发',
}

const RANGES = [7, 30]

function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

/** 毫秒 → 人话（1.4s / 2m18s）。 */
function formatMs(ms: number): string {
  if (!ms || ms < 0) return '—'
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m${String(Math.round(seconds % 60)).padStart(2, '0')}s`
  return `${Math.floor(minutes / 60)}h${String(minutes % 60).padStart(2, '0')}m`
}

/** token 数 → 1.2k / 3.4M。 */
function formatTokens(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

function barHeight(value: number, max: number): string {
  if (value <= 0) return '0'
  return `${Math.max(3, (value / max) * 100)}%`
}

/** 每日双序列柱状图（创建 vs 完成）。 */
function DailyIssuesChart({ created, completed, days }: {
  created: DayCount[]
  completed: DayCount[]
  days: number
}) {
  const max = Math.max(1, ...created.map((item) => item.count), ...completed.map((item) => item.count))
  return (
    <div className="chart" data-testid="chart-issues">
      {created.map((item, index) => {
        const done = completed[index]?.count ?? 0
        const showLabel = days <= 7 || index % 5 === 0 || index === created.length - 1
        return (
          <div className="chart-col" key={item.date} title={`${item.date}：创建 ${item.count}，完成 ${done}`}>
            <div className="chart-bars">
              <span className="chart-bar bar-created" style={{ height: barHeight(item.count, max) }} />
              <span className="chart-bar bar-done" style={{ height: barHeight(done, max) }} />
            </div>
            <span className="chart-x">{showLabel ? item.date.slice(5) : ''}</span>
          </div>
        )
      })}
    </div>
  )
}

/** 每日 token 柱状图（输入 vs 输出）。 */
function DailyTokensChart({ daily, days }: { daily: DayTokens[]; days: number }) {
  const max = Math.max(1, ...daily.map((item) => item.input + item.output))
  return (
    <div className="chart" data-testid="chart-tokens">
      {daily.map((item, index) => {
        const showLabel = days <= 7 || index % 5 === 0 || index === daily.length - 1
        return (
          <div className="chart-col" key={item.date}
            title={`${item.date}：输入 ${item.input}，输出 ${item.output}`}>
            <div className="chart-bars">
              <span className="chart-bar bar-input" style={{ height: barHeight(item.input, max) }} />
              <span className="chart-bar bar-output" style={{ height: barHeight(item.output, max) }} />
            </div>
            <span className="chart-x">{showLabel ? item.date.slice(5) : ''}</span>
          </div>
        )
      })}
    </div>
  )
}

/** 横向排行条（工具调用、失败分类）。 */
function RankList({ items, empty, labels }: {
  items: NamedCount[]
  empty: string
  labels?: Record<string, string>
}) {
  if (!items.length) return <div className="empty-inline">{empty}</div>
  const max = Math.max(1, ...items.map((item) => item.count))
  return (
    <div className="rank-list">
      {items.map((item) => (
        <div className="rank-item" key={item.name}>
          <span className="rank-name">{labels?.[item.name] ?? item.name}</span>
          <span className="rank-count">
            {item.count}
            {item.failed ? <em className="rank-failed">失败 {item.failed}</em> : null}
          </span>
          <span className="rank-bar">
            <span style={{ width: `${(item.count / max) * 100}%` }} />
          </span>
        </div>
      ))}
    </div>
  )
}

/** 资源状态：机器在线数。 */
function MachinesPanel({ machines }: { machines: NamedCount[] }) {
  if (!machines.length) {
    return <div className="empty-inline">还没有登记机器</div>
  }
  return (
    <div className="rank-list">
      {machines.map((item) => (
        <div className="rank-item" key={item.name}>
          <span className="rank-name">
            <StateDot tone={item.name === 'online' ? 'running' : 'muted'} />
            {item.name === 'online' ? '在线' : item.name === 'offline' ? '离线' : '已停用'}
          </span>
          <span className="rank-count">{item.count}</span>
        </div>
      ))}
    </div>
  )
}

export default function Dashboard() {
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [stats, setStats] = useState<StatsOverview | null>(null)
  const [days, setDays] = useState(7)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [nextTasks, nextStats] = await Promise.all([fetchIssues(), fetchStats(days)])
      setTasks(nextTasks)
      setStats(nextStats)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [days])

  useEffect(() => {
    void load()
  }, [load])

  const list = tasks ?? []
  const activeCount = stats?.issues.byStatus.executing ?? list.filter((t) => t.status === 'executing').length
  const failedCount = stats?.issues.byStatus.failed ?? list.filter((t) => t.status === 'failed').length
  const completedCount =
    stats?.issues.byStatus.completed ?? list.filter((t) => t.status === 'completed').length
  const attention = useMemo(
    () => (tasks ?? []).filter((task) => ATTENTION_STATUSES.includes(task.status)).slice(0, 4),
    [tasks],
  )
  const recent = useMemo(
    () =>
      [...(tasks ?? [])]
        .sort(
          (left, right) =>
            new Date(right.updatedAt || right.createdAt || 0).getTime() -
            new Date(left.updatedAt || left.createdAt || 0).getTime(),
        )
        .slice(0, 5),
    [tasks],
  )

  const successRate = stats ? Math.round(stats.runs.successRate * 1000) / 10 : 0
  const tokens = stats ? stats.tokens.input + stats.tokens.output : 0

  return (
    <AppShell
      title="任务大盘"
      breadcrumb="首页"
      actions={
        <div className="button-row">
          <div className="filters" role="group" aria-label="统计窗口">
            {RANGES.map((value) => (
              <button
                key={value}
                type="button"
                className={`filter${days === value ? ' active' : ''}`}
                onClick={() => setDays(value)}
              >
                {value} 天
              </button>
            ))}
          </div>
          <button
            type="button"
            className="button ghost"
            onClick={() => void load()}
            disabled={loading}
          >
            {loading ? '加载中…' : '刷新'}
          </button>
        </div>
      }
    >
      {error ? (
        <div className="notice danger dashboard-error" role="alert">
          <span>加载失败：{error}</span>
          <button type="button" className="button ghost small" onClick={() => void load()}>
            重试
          </button>
        </div>
      ) : null}

      {loading && !stats ? (
        <section className="panel">
          <div className="empty-inline">加载中…</div>
        </section>
      ) : null}

      {stats ? (
        <>
          <div className="metric-grid">
            <MetricCard
              label="进行中"
              value={activeCount}
              foot={`全部 ${stats.issues.total} 个 Issue`}
              to="/task/list"
            />
            <MetricCard
              label="上报完成率"
              value={`${successRate}%`}
              foot={`${stats.runs.succeeded}/${stats.runs.finished} 轮；只算终态`}
              to="/task/list"
            />
            <MetricCard
              label="P95 轮时长"
              value={formatMs(stats.runs.p95Ms)}
              foot={`P50 ${formatMs(stats.runs.p50Ms)} · ${stats.runs.finished} 轮`}
            />
            <MetricCard
              label={`近 ${days} 天 Token`}
              value={formatTokens(tokens)}
              foot={`输入 ${formatTokens(stats.tokens.input)} / 输出 ${formatTokens(stats.tokens.output)}`}
            />
          </div>

          <div className="metric-grid metric-grid-secondary">
            <MetricCard label="已完成" value={completedCount} foot="Agent 上报完成" to="/task/list" />
            <MetricCard label="失败" value={failedCount} foot="需要重试或人工处理" to="/task/list" urgent />
            <MetricCard
              label="工具调用"
              value={stats.toolsByDay.reduce((sum, item) => sum + item.count, 0)}
              foot={`近 ${days} 天合计`}
            />
            <MetricCard
              label="执行轮次"
              value={stats.runs.total}
              foot={`进行中 ${(stats.runs.byStatus.dispatched ?? 0) + (stats.runs.byStatus.running ?? 0)} 轮`}
            />
          </div>

          <div className="dashboard-grid">
            <section className="panel chart-panel">
              <div className="panel-head">
                <div>
                  <h2>每日 Issue</h2>
                  <p>创建按 tasks.created_at，完成按 Agent 上报事件。</p>
                </div>
                <div className="chart-legend">
                  <span><i className="bar-created" />创建</span>
                  <span><i className="bar-done" />完成</span>
                </div>
              </div>
              <DailyIssuesChart created={stats.issues.created} completed={stats.issues.completed} days={days} />
            </section>

            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>失败分类</h2>
                  <p>按执行侧 error_class 聚合。</p>
                </div>
              </div>
              <RankList items={stats.failures} empty="窗口内没有失败的轮次" labels={FAILURE_LABELS} />
            </section>

            <section className="panel chart-panel">
              <div className="panel-head">
                <div>
                  <h2>每日 Token</h2>
                  <p>每轮 Prompt 合计；模型归因取执行时的 Agent 配置。</p>
                </div>
                <div className="chart-legend">
                  <span><i className="bar-input" />输入</span>
                  <span><i className="bar-output" />输出</span>
                </div>
              </div>
              <DailyTokensChart daily={stats.tokens.daily} days={days} />
            </section>

            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>工具调用</h2>
                  <p>取前 8 名，其余归入「其他」。</p>
                </div>
              </div>
              <RankList items={stats.tools} empty="窗口内没有工具调用" labels={{ unknown: '未知' }} />
            </section>

            <section className="panel attention-panel">
              <div className="panel-head">
                <div>
                  <h2>待我处理</h2>
                  <p>这些任务按状态近似识别，暂未接入负责人归属。</p>
                </div>
                <Link className="button subtle" to="/task/list">
                  查看全部
                </Link>
              </div>
              <div className="work-list">
                {attention.length ? (
                  attention.map((task) => {
                    const meta = STATUS_META[task.status]
                    return (
                      <Link
                        className="work-item"
                        key={task.id}
                        to={`/task/detail?id=${encodeURIComponent(task.id)}`}
                      >
                        <StateTag tone={meta.tone}>{meta.label}</StateTag>
                        <div className="work-main">
                          <strong>
                            {shortId(task.id)} {task.name}
                          </strong>
                          <span>
                            {task.workspaceId} · 更新于 {formatTime(task.updatedAt)}
                          </span>
                        </div>
                        <span className="work-meta">— / —</span>
                      </Link>
                    )
                  })
                ) : (
                  <div className="empty-inline">暂无待处理任务</div>
                )}
              </div>
            </section>

            <section className="panel resource-panel">
              <div className="panel-head">
                <div>
                  <h2>资源状态</h2>
                  <p>机器在线情况，来自 machines 表。</p>
                </div>
              </div>
              <MachinesPanel machines={stats.machines} />
            </section>

            <section className="panel recent-panel">
              <div className="panel-head">
                <div>
                  <h2>最近任务</h2>
                  <p>点击任意任务可恢复到驾驶舱上下文。</p>
                </div>
              </div>
              {recent.length ? (
                <div className="recent-table">
                  <div className="table-row table-head-row">
                    <span>任务</span>
                    <span>状态</span>
                    <span>进度</span>
                    <span>机器</span>
                    <span>Agent</span>
                    <span>最近活动</span>
                  </div>
                  {recent.map((task) => {
                    const meta = STATUS_META[task.status]
                    return (
                      <Link
                        className="table-row"
                        key={task.id}
                        to={`/task/detail?id=${encodeURIComponent(task.id)}`}
                      >
                        <span className="cell-task">
                          <strong>
                            {shortId(task.id)} {task.name}
                          </strong>
                          <span>{task.workspaceId}</span>
                        </span>
                        <span className="cell-status">
                          <StateDot tone={meta.tone} /> {meta.label}
                        </span>
                        <span className="cell-muted">—</span>
                        <span className="cell-muted">—</span>
                        <span className="cell-muted">—</span>
                        <span className="cell-muted">{formatTime(task.updatedAt)}</span>
                      </Link>
                    )
                  })}
                </div>
              ) : (
                <EmptyState
                  title="暂无任务"
                  description="本地后端还没有返回任务数据，创建任务后会展示在这里。"
                />
              )}
            </section>
          </div>
        </>
      ) : null}
    </AppShell>
  )
}
