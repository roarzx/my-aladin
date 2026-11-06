import { useCallback, useEffect, useState } from 'react'

import { fetchIssues, type Task, type TaskStatus } from '../api/issues'
import AppShell from '../components/AppShell'
import { StateTag, type TagTone } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-issues.css'

const STATUS_META: Record<TaskStatus, { label: string; tone: TagTone }> = {
  pending: { label: '待处理', tone: 'pending' },
  plan_confirming: { label: '待我确认', tone: 'review' },
  executing: { label: '执行中', tone: 'running' },
  paused: { label: '已暂停', tone: 'failed' },
  cancelling: { label: '取消中', tone: 'queued' },
  chatting: { label: '对话中', tone: 'running' },
  cancelled: { label: '已取消', tone: 'cancelled' },
  failed: { label: '已失败', tone: 'failed' },
}

/** 看板四列，分组规则与参考站 taskBoardGroup 一致（本地状态做等价映射）。 */
const BOARD_GROUPS: Array<{
  key: 'review' | 'running' | 'failed' | 'done'
  title: string
  desc: string
  statuses: TaskStatus[]
}> = [
  {
    key: 'review',
    title: '待处理',
    desc: '等待处理、补充输入或指定成员确认方案。',
    statuses: ['pending', 'plan_confirming'],
  },
  {
    key: 'running',
    title: '执行中',
    desc: 'Agent 正在推进步骤和输出日志。',
    statuses: ['executing', 'chatting', 'cancelling'],
  },
  {
    key: 'failed',
    title: '异常处理',
    desc: '失败、超时或资源不可用任务。',
    statuses: ['failed', 'paused'],
  },
  {
    key: 'done',
    title: '已完成',
    desc: '可查看产出或创建类似任务。',
    statuses: ['cancelled'],
  },
]

type BoardGroupKey = (typeof BOARD_GROUPS)[number]['key']
type StatusFilter = 'all' | 'need' | 'awaiting_my_confirmation'

const FILTERS: Array<{ key: StatusFilter; label: string }> = [
  { key: 'all', label: '全部状态' },
  { key: 'need', label: '待我处理' },
  { key: 'awaiting_my_confirmation', label: '待我确认' },
]

/** 「待我处理」近似识别：需要人介入的状态集合。 */
const NEED_ATTENTION: TaskStatus[] = ['pending', 'plan_confirming', 'paused', 'failed']

function boardGroupOf(status: TaskStatus): BoardGroupKey | null {
  const group = BOARD_GROUPS.find((item) => item.statuses.includes(status))
  return group ? group.key : null
}

function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

function datePart(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (input: number) => String(input).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

export default function IssueList() {
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [keyword, setKeyword] = useState('')
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')

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

  const term = keyword.trim().toLowerCase()
  const filtered = (tasks ?? []).filter((task) => {
    if (statusFilter === 'need' && !NEED_ATTENTION.includes(task.status)) return false
    if (statusFilter === 'awaiting_my_confirmation' && task.status !== 'plan_confirming') {
      return false
    }
    if (startDate || endDate) {
      const day = datePart(task.updatedAt)
      if (startDate && (!day || day < startDate)) return false
      if (endDate && (!day || day > endDate)) return false
    }
    if (!term) return true
    return [task.name, task.id, task.workspaceId, task.planBranch ?? '']
      .join(' ')
      .toLowerCase()
      .includes(term)
  })

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
          <div className="task-date-range" aria-label="最近执行更新时间筛选">
            <span className="task-date-label">最近执行更新</span>
            <div className="task-date-fields">
              <input
                type="date"
                className="task-date-input"
                aria-label="开始日期"
                value={startDate}
                onChange={(event) => setStartDate(event.target.value)}
              />
              <span className="task-date-arrow" aria-hidden>
                →
              </span>
              <input
                type="date"
                className="task-date-input"
                aria-label="结束日期"
                value={endDate}
                onChange={(event) => setEndDate(event.target.value)}
              />
            </div>
          </div>
          <div className="search">
            <input
              type="search"
              aria-label="搜索任务"
              placeholder="搜索任务 / 仓库 / Agent"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
            />
          </div>
          <button
            type="button"
            className="button ghost"
            onClick={() => void load()}
            disabled={loading}
          >
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

      <section className="task-waterfall">
        {BOARD_GROUPS.map((group) => {
          const items = filtered.filter((task) => boardGroupOf(task.status) === group.key)
          return (
            <div className={`waterfall-column ${group.key}`} key={group.key}>
              <div className="waterfall-head">
                <div>
                  <h2>{group.title}</h2>
                  <p>{group.desc}</p>
                </div>
                <div className="waterfall-head-actions">
                  <span className={`count-badge ${group.key}`}>{items.length}</span>
                </div>
              </div>

              {items.map((task) => {
                const meta = STATUS_META[task.status]
                return (
                  <Link
                    className={`task-card ${meta.tone}-card`}
                    key={task.id}
                    to={`/task/detail?id=${encodeURIComponent(task.id)}`}
                  >
                    <div className="task-card-top">
                      <StateTag tone={meta.tone}>{meta.label}</StateTag>
                      <span className="task-card-meta">
                        <span className="task-id">{shortId(task.id)}</span>
                      </span>
                    </div>
                    <h3>{task.name}</h3>
                    <p className="task-summary">
                      {task.planBranch ? `分支 ${task.planBranch}` : `工作区 ${task.workspaceId}`}
                    </p>
                    <p className="task-creator">更新于 {formatTime(task.updatedAt)}</p>
                  </Link>
                )
              })}

              {items.length === 0 ? (
                <div className="empty-inline">
                  {!tasks && loading ? '加载中…' : '暂无任务'}
                </div>
              ) : null}
            </div>
          )
        })}
      </section>
    </AppShell>
  )
}
