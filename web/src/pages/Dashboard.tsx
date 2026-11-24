import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'

import { fetchIssues, type Task, type TaskStatus } from '../api/issues'
import { fetchStats, type DayCount, type DayTokens, type NamedCount, type StatsOverview } from '../api/stats'
import AppShell from '../components/AppShell'
import { EmptyState, MetricCard, StateDot, StateTag, type TagTone } from '../components/ui'
import { Link, useRouter } from '../router'
import '../styles/pages-issues.css'

/**
 * 平台总览（监控的第一块）。
 *
 * 只回答「平台整体怎么样」：任务量、完成率、错误率、耗时、Worker 可用性。
 * Agent 级拆分在「执行分析」，单次执行的完整轨迹在 Issue 驾驶舱的「记录」。
 */

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
export function formatMs(ms: number): string {
  if (!ms || ms < 0) return '—'
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m${String(Math.round(seconds % 60)).padStart(2, '0')}s`
  return `${Math.floor(minutes / 60)}h${String(minutes % 60).padStart(2, '0')}m`
}

/** token 数 → 1.2k / 3.4M。 */
export function formatTokens(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

function percent(ratio: number): string {
  return `${Math.round(ratio * 1000) / 10}%`
}

function barHeight(value: number, max: number): string {
  if (value <= 0) return '0'
  return `${Math.max(3, (value / max) * 100)}%`
}

/** 每日双序列柱状图（创建 vs 完成）。 */
export function DailyIssuesChart({ created, completed, days }: {
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
export function DailyTokensChart({ daily, days }: { daily: DayTokens[]; days: number }) {
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

/** 机器状态 + 在线率。 */
function MachinesPanel({ machines, availability }: { machines: NamedCount[]; availability: number }) {
  if (!machines.length) {
    return <div className="empty-inline">还没有登记机器</div>
  }
  const label = (name: string) =>
    name === 'online' ? '在线' : name === 'offline' ? '离线' : name === 'disabled' ? '已停用' : name
  return (
    <div className="machine-panel">
      <div className="machine-availability">
        <strong>{percent(availability)}</strong>
        <span>在线率</span>
      </div>
      <div className="rank-list">
        {machines.map((item) => (
          <div className="rank-item" key={item.name}>
            <span className="rank-name">
              <StateDot tone={item.name === 'online' ? 'running' : 'muted'} />
              {label(item.name)}
            </span>
            <span className="rank-count">{item.count}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

export default function Dashboard() {
  const { navigate } = useRouter()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [stats, setStats] = useState<StatsOverview | null>(null)
  const [days, setDays] = useState(7)
  const [issueId, setIssueId] = useState('')
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

  const openIssue = (event: FormEvent) => {
    event.preventDefault()
    const id = issueId.trim()
    if (!id) return
    navigate(`/issues/${encodeURIComponent(id)}`)
  }

  const online = stats?.machines.find((item) => item.name === 'online')?.count ?? 0
  const machinesTotal = (stats?.machines ?? []).reduce((sum, item) => sum + item.count, 0)

  return (
    <AppShell
      title="平台总览"
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
              value={stats.issues.byStatus.executing ?? 0}
              foot={`全部 ${stats.issues.total} 个 Issue`}
              to="/task/list"
            />
            <MetricCard
              label="上报完成率"
              value={percent(stats.runs.successRate)}
              foot={`${stats.runs.succeeded}/${stats.runs.finished} 轮；只算终态`}
            />
            <MetricCard
              label="失败率"
              value={percent(stats.runs.errorRate)}
              foot={`${stats.runs.failed} 轮失败 · ${stats.runs.cancelled} 轮取消`}
              urgent={stats.runs.failed > 0}
            />
            <MetricCard
              label="P95 轮时长"
              value={formatMs(stats.runs.p95Ms)}
              foot={`P50 ${formatMs(stats.runs.p50Ms)} · ${stats.runs.finished} 轮`}
            />
          </div>

          <div className="metric-grid metric-grid-secondary">
            <MetricCard
              label={`近 ${days} 天 Token`}
              value={formatTokens(stats.tokens.input + stats.tokens.output)}
              foot={`输入 ${formatTokens(stats.tokens.input)} / 输出 ${formatTokens(stats.tokens.output)}`}
            />
            <MetricCard
              label="Worker 可用性"
              value={percent(stats.availability)}
              foot={`${online}/${machinesTotal} 台机器在线`}
              to="/machines"
            />
            <MetricCard
              label="执行轮次"
              value={stats.runs.total}
              foot={`其中 ${stats.runs.retried} 轮非首轮`}
            />
            <MetricCard
              label="已完成"
              value={stats.issues.byStatus.completed ?? 0}
              foot="Agent 上报完成"
              to="/task/list"
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

            <section className="panel resource-panel">
              <div className="panel-head">
                <div>
                  <h2>资源状态</h2>
                  <p>机器在线情况，来自 machines 表。</p>
                </div>
              </div>
              <MachinesPanel machines={stats.machines} availability={stats.availability} />
            </section>

            <section className="panel chart-panel wide">
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

            <section className="panel attention-panel wide">
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

            <section className="panel recent-panel">
              <div className="panel-head">
                <div>
                  <h2>最近任务</h2>
                  <p>点击进入驾驶舱；也可以直接按 Issue ID 打开。</p>
                </div>
                <form className="id-jump" onSubmit={openIssue}>
                  <input
                    className="input"
                    placeholder="按 Issue ID 打开执行轨迹"
                    value={issueId}
                    onChange={(event) => setIssueId(event.target.value)}
                    aria-label="Issue ID"
                  />
                  <button type="submit" className="button subtle small" disabled={!issueId.trim()}>
                    打开
                  </button>
                </form>
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
