import { useCallback, useEffect, useState } from 'react'

import { fetchIssues, type Task, type TaskStatus } from '../api/issues'
import AppShell from '../components/AppShell'
import { EmptyState, MetricCard, StateDot, StateTag, type TagTone } from '../components/ui'
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

/** 参考站按状态近似识别「待我处理」：计划确认、失败重试、输入请求。 */
const ATTENTION_STATUSES: TaskStatus[] = ['plan_confirming', 'failed', 'paused']

function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

export default function Dashboard() {
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

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

  const list = tasks ?? []
  const activeCount = list.filter(
    (task) => task.status === 'executing' || task.status === 'chatting',
  ).length
  const failedCount = list.filter((task) => task.status === 'failed').length
  const cancelledCount = list.filter((task) => task.status === 'cancelled').length
  const attention = list
    .filter((task) => ATTENTION_STATUSES.includes(task.status))
    .slice(0, 4)
  const recent = [...list]
    .sort(
      (left, right) =>
        new Date(right.updatedAt || right.createdAt || 0).getTime() -
        new Date(left.updatedAt || left.createdAt || 0).getTime(),
    )
    .slice(0, 5)

  return (
    <AppShell
      title="任务大盘"
      breadcrumb="首页"
      actions={
        <button
          type="button"
          className="button ghost"
          onClick={() => void load()}
          disabled={loading}
        >
          {loading ? '加载中…' : '刷新'}
        </button>
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

      {loading && !tasks ? (
        <section className="panel">
          <div className="empty-inline">加载中…</div>
        </section>
      ) : null}

      {tasks ? (
        <>
          <div className="metric-grid">
            <MetricCard
              label="全部 Issue"
              value={list.length}
              foot="本地后端实时任务数据"
              to="/task/list"
            />
            <MetricCard
              label="进行中"
              value={activeCount}
              foot={`${activeCount} 个正在推进`}
              to="/task/list"
            />
            <MetricCard
              label="失败"
              value={failedCount}
              foot="失败步骤需要重试或重新规划"
              to="/task/list"
              urgent
            />
            <MetricCard
              label="已取消"
              value={cancelledCount}
              foot="用户取消或中断的任务"
              to="/task/list"
            />
          </div>

          <div className="dashboard-grid">
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
                  <p>机器与 Agent 的实时调度能力。</p>
                </div>
              </div>
              <EmptyState
                title="暂未接入"
                description="本地后端尚未提供该能力。接入机器与 Agent 接口后，这里会展示在线数与空闲情况。"
              />
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
