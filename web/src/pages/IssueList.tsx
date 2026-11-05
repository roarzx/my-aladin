import { useCallback, useEffect, useState } from 'react'

import { fetchIssues, type Task } from '../api/issues'

const STATUS_LABELS: Record<string, string> = {
  pending: '待规划',
  plan_confirming: '待确认计划',
  executing: '执行中',
  paused: '已暂停',
  cancelling: '取消中',
  chatting: '对话中',
  cancelled: '已取消',
  failed: '已失败',
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', { hour12: false })
}

export default function IssueList() {
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(async () => {
    setError(null)
    try {
      setTasks(await fetchIssues())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  return (
    <main className="app">
      <header className="topbar">
        <h1>阿拉丁 · 任务</h1>
        <div className="actions">
          <button type="button" onClick={() => void reload()}>
            刷新
          </button>
          <button
            type="button"
            onClick={() => {
              localStorage.removeItem('aladdin_token')
              window.location.reload()
            }}
          >
            切换令牌
          </button>
        </div>
      </header>

      {error ? (
        <p role="alert" className="error">
          加载失败：{error}
        </p>
      ) : null}

      {!tasks && !error ? <p>加载中…</p> : null}

      {tasks && tasks.length === 0 ? (
        <p className="empty">还没有任务。可通过 <code>POST /workspaces/{'{wid}'}/issues</code> 创建。</p>
      ) : null}

      {tasks && tasks.length > 0 ? (
        <table className="issue-table">
          <thead>
            <tr>
              <th>任务</th>
              <th>状态</th>
              <th>工作区</th>
              <th>更新时间</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((task) => (
              <tr key={task.id}>
                <td>
                  <a href={`/?taskId=${encodeURIComponent(task.id)}`}>{task.name}</a>
                </td>
                <td>
                  <span className={`status status-${task.status}`}>
                    {STATUS_LABELS[task.status] ?? task.status}
                  </span>
                </td>
                <td className="muted">{task.workspaceId}</td>
                <td className="muted">{formatTime(task.updatedAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </main>
  )
}
