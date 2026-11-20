import { useCallback, useEffect, useMemo, useState } from 'react'

import { fetchIssues, type Task, type TaskStatus } from '../api/issues'
import AppShell from '../components/AppShell'
import { StateTag, type TagTone } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-issues.css'

const STATUS_META: Record<TaskStatus, { label: string; tone: TagTone }> = {
  pending: { label: '待开始', tone: 'pending' },
  executing: { label: '执行中', tone: 'running' },
  paused: { label: '已阻塞', tone: 'paused' },
  completed: { label: '已完成', tone: 'done' },
  cancelled: { label: '已取消', tone: 'cancelled' },
  failed: { label: '已失败', tone: 'failed' },
}

/** 看板四列：待处理 / 执行中 / 异常处理 / 已完成。 */
const BOARD_GROUPS: Array<{
  key: 'review' | 'running' | 'failed' | 'done'
  title: string
  desc: string
  empty: string
  statuses: TaskStatus[]
}> = [
  {
    key: 'review',
    title: '待处理',
    desc: '还没有交给 Agent。',
    empty: '没有待开始的 Issue',
    statuses: ['pending'],
  },
  {
    key: 'running',
    title: '执行中',
    desc: 'Agent 正在长会话里推进。',
    empty: '当前没有执行中的 Issue',
    statuses: ['executing'],
  },
  {
    key: 'failed',
    title: '异常处理',
    desc: 'Agent 上报阻塞，或本轮执行失败。',
    empty: '没有需要处理的异常',
    statuses: ['failed', 'paused'],
  },
  {
    key: 'done',
    title: '已完成',
    desc: '可查看产物或继续对话。',
    empty: '还没有完成的 Issue',
    statuses: ['completed', 'cancelled'],
  },
]

type BoardGroupKey = (typeof BOARD_GROUPS)[number]['key']
type StatusFilter = 'all' | 'need' | 'blocked'
type TimeRange = 'all' | 'today' | 'week' | 'month'

const FILTERS: Array<{ key: StatusFilter; label: string }> = [
  { key: 'all', label: '全部状态' },
  { key: 'need', label: '待我处理' },
  { key: 'blocked', label: 'Agent 阻塞' },
]

const TIME_RANGES: Array<{ key: TimeRange; label: string }> = [
  { key: 'all', label: '全部时间' },
  { key: 'today', label: '今天更新' },
  { key: 'week', label: '近 7 天' },
  { key: 'month', label: '近 30 天' },
]

/** 「待我处理」：需要人介入的状态。 */
const NEED_ATTENTION: TaskStatus[] = ['pending', 'paused', 'failed']

const RUN_STATUS_LABELS: Record<string, { label: string; tone: TagTone }> = {
  dispatched: { label: '已派发', tone: 'queued' },
  running: { label: '进行中', tone: 'running' },
  completed: { label: '完成', tone: 'done' },
  failed: { label: '失败', tone: 'failed' },
  cancelled: { label: '已取消', tone: 'cancelled' },
}

function boardGroupOf(status: TaskStatus): BoardGroupKey | null {
  const group = BOARD_GROUPS.find((item) => item.statuses.includes(status))
  return group ? group.key : null
}

/** 相对时间：列表卡片上「多久没动」比绝对时间更有用。 */
export function relativeTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  const diff = Date.now() - date.getTime()
  if (diff < 0) return '刚刚'
  const minute = 60_000
  if (diff < minute) return '刚刚'
  if (diff < 60 * minute) return `${Math.floor(diff / minute)} 分钟前`
  if (diff < 24 * 60 * minute) return `${Math.floor(diff / (60 * minute))} 小时前`
  if (diff < 30 * 24 * 60 * minute) return `${Math.floor(diff / (24 * 60 * minute))} 天前`
  return date.toLocaleDateString('zh-CN')
}

function withinRange(value: string, range: TimeRange): boolean {
  if (range === 'all') return true
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return false
  const days = range === 'today' ? 1 : range === 'week' ? 7 : 30
  const start = new Date()
  start.setHours(0, 0, 0, 0)
  start.setDate(start.getDate() - (days - 1))
  return date.getTime() >= start.getTime()
}

function matches(task: Task, term: string): boolean {
  if (!term) return true
  return [task.name, task.id, task.requirement ?? '', task.agentName ?? '', task.planBranch ?? '']
    .join(' ')
    .toLowerCase()
    .includes(term)
}

function TaskCard({ task }: { task: Task }) {
  const meta = STATUS_META[task.status]
  const run = task.latestRunStatus ? RUN_STATUS_LABELS[task.latestRunStatus] : null
  // 上一轮的结果只在「已完成/失败」时值得强调，执行中不用重复说。
  const showRun = run && task.latestRunStatus !== 'dispatched' && task.latestRunStatus !== 'running'

  return (
    <Link className={`task-card ${meta.tone}-card`} to={`/task/detail?id=${encodeURIComponent(task.id)}`}>
      <div className="task-card-top">
        <StateTag tone={meta.tone}>{meta.label}</StateTag>
        <span className="task-card-time" title={task.updatedAt}>
          {relativeTime(task.updatedAt)}
        </span>
      </div>

      <h3>{task.name}</h3>

      {task.requirement ? <p className="task-summary">{task.requirement}</p> : null}

      <div className="task-card-foot">
        <span className="task-agent" title={task.agentName || '未分配 Agent'}>
          <i className="task-agent-dot" aria-hidden />
          {task.agentName || '未分配 Agent'}
        </span>
        {showRun ? (
          <span className={`task-run ${run.tone}`} title={`上一轮：${run.label}`}>
            上轮 {run.label}
          </span>
        ) : null}
      </div>
    </Link>
  )
}

function SkeletonCard() {
  return (
    <div className="task-card skeleton" aria-hidden>
      <div className="skeleton-line short" />
      <div className="skeleton-line" />
      <div className="skeleton-line medium" />
    </div>
  )
}

export default function IssueList() {
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [timeRange, setTimeRange] = useState<TimeRange>('all')
  const [keyword, setKeyword] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setTasks(await fetchIssues())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const filtered = useMemo(() => {
    const term = keyword.trim().toLowerCase()
    return (tasks ?? []).filter((task) => {
      if (statusFilter === 'need' && !NEED_ATTENTION.includes(task.status)) return false
      if (statusFilter === 'blocked' && task.status !== 'paused') return false
      if (!withinRange(task.updatedAt, timeRange)) return false
      return matches(task, term)
    })
  }, [tasks, statusFilter, timeRange, keyword])

  const total = filtered.length

  return (
    <AppShell title="Issue 列表" breadcrumb="我的 Issue">
      <div className="toolbar issue-board-toolbar">
        <div className="filters" role="group" aria-label="按状态筛选">
          {FILTERS.map((item) => (
            <button
              key={item.key}
              type="button"
              className={statusFilter === item.key ? 'filter active' : 'filter'}
              onClick={() => setStatusFilter(item.key)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <div className="toolbar-actions">
          <div className="search">
            <input
              type="search"
              aria-label="搜索任务"
              placeholder="搜索标题 / 需求 / Agent"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
            />
          </div>
          <label className="issue-time-filter">
            <span className="sr-only">最近更新时间</span>
            <select
              aria-label="最近更新时间"
              value={timeRange}
              onChange={(event) => setTimeRange(event.target.value as TimeRange)}
            >
              {TIME_RANGES.map((item) => (
                <option key={item.key} value={item.key}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="button ghost" onClick={() => void load()} disabled={loading}>
            {loading ? '刷新中…' : '刷新'}
          </button>
          <Link className="button primary" to="/task/create">
            创建 issue
          </Link>
        </div>
      </div>

      {error ? (
        <div className="notice danger dashboard-error" role="alert">
          <span>加载失败：{error}</span>
          <button type="button" className="button ghost small" onClick={() => void load()}>
            重试
          </button>
        </div>
      ) : null}

      {!error && tasks && total === 0 ? (
        <div className="issue-board-empty">
          没有匹配的 Issue
          {statusFilter !== 'all' || timeRange !== 'all' || keyword.trim() ? '，换个筛选条件试试' : ''}
        </div>
      ) : null}

      <section className="task-waterfall" aria-busy={loading}>
        {BOARD_GROUPS.map((group) => {
          const items = filtered.filter((task) => boardGroupOf(task.status) === group.key)
          return (
            <div className={`waterfall-column ${group.key}`} key={group.key}>
              <div className="waterfall-head">
                <div className="waterfall-head-main">
                  <span className={`waterfall-dot ${group.key}`} aria-hidden />
                  <h2>{group.title}</h2>
                  <span className={`count-badge ${group.key}`}>{items.length}</span>
                </div>
                <p>{group.desc}</p>
              </div>

              <div className="waterfall-body">
                {loading && !tasks
                  ? [0, 1].map((index) => <SkeletonCard key={index} />)
                  : items.map((task) => <TaskCard key={task.id} task={task} />)}

                {!loading && items.length === 0 && tasks ? (
                  <div className="waterfall-empty">{group.empty}</div>
                ) : null}
              </div>
            </div>
          )
        })}
      </section>
    </AppShell>
  )
}
