import { useCallback, useEffect, useState } from 'react'

import { fetchIssues, type Task, type TaskStatus } from '../api/issues'
import AppShell from '../components/AppShell'
import { EmptyState, StateTag, type TagTone } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-issues.css'

const STATUS_META: Record<TaskStatus, { label: string; tone: TagTone }> = {
  pending: { label: '待规划', tone: 'pending' },
  plan_confirming: { label: '待确认计划', tone: 'review' },
  executing: { label: '执行中', tone: 'running' },
  paused: { label: '已暂停', tone: 'input' },
  cancelling: { label: '取消中', tone: 'queued' },
  chatting: { label: '对话中', tone: 'review' },
  cancelled: { label: '已取消', tone: 'cancelled' },
  failed: { label: '已失败', tone: 'failed' },
}

const STATUS_ORDER: TaskStatus[] = [
  'pending',
  'plan_confirming',
  'executing',
  'paused',
  'cancelling',
  'chatting',
  'cancelled',
  'failed',
]

type StatusFilter = TaskStatus | 'all'

function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

export default function IssueList() {
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
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

  const term = keyword.trim().toLowerCase()
  const filtered = (tasks ?? []).filter((task) => {
    if (status !== 'all' && task.status !== status) return false
    if (!term) return true
    return task.name.toLowerCase().includes(term) || task.id.toLowerCase().includes(term)
  })

  return (
    <AppShell title="Issue 列表" breadcrumb="我的 Issue">
      <div className="toolbar">
        <div className="filters" role="group" aria-label="按状态筛选">
          <button
            type="button"
            className={status === 'all' ? 'filter active' : 'filter'}
            onClick={() => setStatus('all')}
          >
            全部
          </button>
          {STATUS_ORDER.map((value) => (
            <button
              key={value}
              type="button"
              className={status === value ? 'filter active' : 'filter'}
              onClick={() => setStatus(value)}
            >
              {STATUS_META[value].label}
            </button>
          ))}
        </div>
        <div className="button-row">
          <div className="search">
            <input
              type="search"
              aria-label="搜索任务"
              placeholder="搜索名称或 ID"
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
            {loading ? '加载中…' : '刷新'}
          </button>
          <Link className="button primary" to="/task/create">
            创建任务
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

      {loading && !tasks ? (
        <section className="panel">
          <div className="empty-inline">加载中…</div>
        </section>
      ) : null}

      {tasks ? (
        filtered.length ? (
          <div className="data-table issue-list-table">
            <div className="data-table-row table-head">
              <span>任务</span>
              <span>状态</span>
              <span>工作区</span>
              <span>进度</span>
              <span>更新时间</span>
            </div>
            {filtered.map((task) => {
              const meta = STATUS_META[task.status]
              return (
                <Link
                  className="data-table-row clickable"
                  key={task.id}
                  to={`/task/detail?id=${encodeURIComponent(task.id)}`}
                >
                  <span className="cell-task">
                    <strong>{task.name}</strong>
                    <span>{shortId(task.id)}</span>
                  </span>
                  <span className="cell-status">
                    <StateTag tone={meta.tone}>{meta.label}</StateTag>
                  </span>
                  <span className="cell-muted">{task.workspaceId}</span>
                  <span className="cell-muted">—</span>
                  <span className="cell-muted">{formatTime(task.updatedAt)}</span>
                </Link>
              )
            })}
          </div>
        ) : tasks.length ? (
          <EmptyState
            title="没有匹配的 Issue"
            description="调整状态筛选或搜索关键词后再试。"
            action={
              <button
                type="button"
                className="button ghost"
                onClick={() => {
                  setStatus('all')
                  setKeyword('')
                }}
              >
                清除筛选
              </button>
            }
          />
        ) : (
          <EmptyState
            title="还没有任务"
            description="通过创建任务发起第一轮研发流程。"
            action={
              <Link className="button primary" to="/task/create">
                创建任务
              </Link>
            }
          />
        )
      ) : null}
    </AppShell>
  )
}
