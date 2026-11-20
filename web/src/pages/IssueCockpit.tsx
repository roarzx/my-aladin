import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'

import {
  cancelIssue,
  downloadArtifact,
  fetchArtifacts,
  fetchIssue,
  sendMessage,
  startIssue,
  type Artifact,
  type EventPage,
  type IssueDetail,
  type IssueRun,
  type TaskStatus,
} from '../api/issues'
import AppShell from '../components/AppShell'
import AuditTimeline from '../components/AuditTimeline'
import { MarkdownView } from '../components/markdown'
import { EmptyState, StateDot, StateTag, toast, type TagTone } from '../components/ui'
import { useEventStream } from '../hooks/useEventStream'
import { Link } from '../router'
import '../styles/pages-issues.css'

export interface CockpitApi {
  fetchIssue: typeof fetchIssue
  /** 任务产物列表（GET /issues/{id}/artifacts）。 */
  fetchArtifacts?: typeof fetchArtifacts
  /** 产物下载。 */
  downloadArtifact?: typeof downloadArtifact
  /** 发消息（POST /issues/{id}/messages）。 */
  sendMessage?: typeof sendMessage
  /** 开始/继续一轮（POST /issues/{id}/start）。 */
  startIssue?: typeof startIssue
  /** 取消（POST /issues/{id}/cancel）。 */
  cancelIssue?: typeof cancelIssue
}

const defaultApi: CockpitApi = {
  fetchIssue,
  fetchArtifacts,
  downloadArtifact,
  sendMessage,
  startIssue,
  cancelIssue,
}

export interface IssueCockpitProps {
  taskId: string
  api?: CockpitApi
  /** 可注入的事件拉取函数（测试用）；默认走 HTTP 轮询。 */
  eventsFetcher?: (taskId: string, after: number) => Promise<EventPage>
}

const STATUS_LABELS: Record<TaskStatus, string> = {
  pending: '待开始',
  executing: '执行中',
  paused: '已阻塞',
  completed: '已完成',
  cancelled: '已取消',
  failed: '已失败',
}

const STATUS_TONES: Record<TaskStatus, TagTone> = {
  pending: 'pending',
  executing: 'running',
  paused: 'paused',
  completed: 'done',
  cancelled: 'cancelled',
  failed: 'failed',
}

/** 会话里的条目：用户消息、Agent 回复、状态变更、失败与提问。 */
type ThreadItem =
  | { key: string; kind: 'user' | 'assistant'; text: string; at: string }
  | { key: string; kind: 'status'; from: string; to: string; reason: string; at: string }
  | { key: string; kind: 'failed'; reason: string; errorClass: string; at: string }
  | { key: string; kind: 'input'; message: string; at: string }

const STATUS_NAMES: Record<string, string> = {
  pending: '待开始',
  executing: '执行中',
  paused: '已阻塞',
  completed: '已完成',
  cancelled: '已取消',
  failed: '已失败',
}

function statusName(value: string): string {
  return STATUS_NAMES[value] ?? value
}

function payloadOf(event: { payload: unknown }): Record<string, unknown> {
  const payload = event.payload
  if (payload && typeof payload === 'object' && !Array.isArray(payload)) {
    return payload as Record<string, unknown>
  }
  return {}
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', { hour12: false })
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function fileBadge(path: string): string {
  const name = path.split('/').pop() ?? path
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toUpperCase().slice(0, 4) : 'FILE'
}

/** 把事件流折叠成会话时间线。 */
function buildThread(events: { seq: number; eventId: string; kind: string; payload: unknown; createdAt: string }[]): ThreadItem[] {
  const sorted = [...events].sort((a, b) => a.seq - b.seq)
  const items: ThreadItem[] = []
  for (const event of sorted) {
    const payload = payloadOf(event)
    const key = event.eventId || `${event.kind}-${event.seq}`
    switch (event.kind) {
      case 'message.user':
        items.push({ key, kind: 'user', text: str(payload.text), at: event.createdAt })
        break
      case 'message.assistant':
        items.push({ key, kind: 'assistant', text: str(payload.text), at: event.createdAt })
        break
      case 'issue.status':
        items.push({
          key,
          kind: 'status',
          from: str(payload.from),
          to: str(payload.to),
          reason: str(payload.reason),
          at: event.createdAt,
        })
        break
      case 'turn.failed':
        items.push({
          key,
          kind: 'failed',
          reason: str(payload.reason),
          errorClass: str(payload.error_class),
          at: event.createdAt,
        })
        break
      case 'issue.input_requested':
        items.push({ key, kind: 'input', message: str(payload.message), at: event.createdAt })
        break
      default:
        break
    }
  }
  return items
}

function ThreadBubble({ item }: { item: ThreadItem }): ReactNode {
  if (item.kind === 'user') {
    return (
      <article className="msg" data-testid="thread-user">
        <div className="msg-head">
          <span className="role-badge">你</span>
          <span className="msg-time">{formatTime(item.at)}</span>
        </div>
        <div className="msg-body">{item.text}</div>
      </article>
    )
  }
  if (item.kind === 'assistant') {
    return (
      <article className="msg" data-testid="thread-assistant">
        <div className="msg-head">
          <span className="role-badge">Agent</span>
          <span className="msg-time">{formatTime(item.at)}</span>
        </div>
        <div className="msg-body">
          <MarkdownView text={item.text} />
        </div>
      </article>
    )
  }
  if (item.kind === 'input') {
    return (
      <div className="thread-system" data-testid="thread-input">
        <StateDot tone="paused" />
        <span>Agent 请求输入：{item.message}</span>
        <span className="msg-time">{formatTime(item.at)}</span>
      </div>
    )
  }
  if (item.kind === 'status') {
    return (
      <div className="thread-system" data-testid="thread-status">
        <StateDot tone="review" />
        <span>
          状态：{statusName(item.from)} → {statusName(item.to)}
          {item.reason ? `（${item.reason}）` : ''}
        </span>
        <span className="msg-time">{formatTime(item.at)}</span>
      </div>
    )
  }
  if (item.kind === 'failed') {
    return (
      <div className="thread-system failed" data-testid="thread-failed">
        <StateDot tone="failed" />
        <span>
          本轮执行失败{item.errorClass ? `（${item.errorClass}）` : ''}：{item.reason}
        </span>
        <span className="msg-time">{formatTime(item.at)}</span>
      </div>
    )
  }
  return null
}

export default function IssueCockpit({ taskId, api, eventsFetcher }: IssueCockpitProps) {
  const client = useMemo<CockpitApi>(() => ({ ...defaultApi, ...api }), [api])

  const [detail, setDetail] = useState<IssueDetail | null>(null)
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)
  const [tab, setTab] = useState<'chat' | 'records'>('chat')
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const stream = useEventStream(taskId, 0, eventsFetcher ? { fetcher: eventsFetcher } : {})

  const reload = useCallback(async () => {
    try {
      const fresh = await client.fetchIssue(taskId)
      setDetail(fresh)
      setLoadError(null)
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err))
    }
    if (client.fetchArtifacts) {
      try {
        setArtifacts(await client.fetchArtifacts(taskId))
      } catch {
        setArtifacts([])
      }
    }
  }, [client, taskId])

  useEffect(() => {
    void reload()
  }, [reload])

  // 事件流变化时刷新详情：状态由 Agent 上报，轮询才能跟上。
  useEffect(() => {
    if (stream.events.length === 0) return
    void reload()
  }, [stream.events.length, reload])

  const onDownload = useCallback(
    async (item: Artifact) => {
      if (!client.downloadArtifact) return
      try {
        await client.downloadArtifact(taskId, item)
      } catch (err) {
        setActionError(err instanceof Error ? err.message : String(err))
      }
    },
    [client, taskId],
  )

  const thread = useMemo(() => buildThread(stream.events), [stream.events])

  if (!taskId) {
    return (
      <AppShell title="Issue">
        <EmptyState title="缺少 Issue" description="请从 Issue 列表进入详情页。" />
      </AppShell>
    )
  }

  if (loadError && !detail) {
    return (
      <AppShell title="Issue">
        <div className="notice danger">加载失败：{loadError}</div>
      </AppShell>
    )
  }

  if (!detail) {
    return (
      <AppShell title="Issue">
        <div className="empty-inline">加载中…</div>
      </AppShell>
    )
  }

  const task = detail.task
  const running = task.status === 'executing'
  const terminal = task.status === 'cancelled' || task.status === 'failed'
  const latestRun: IssueRun | null = detail.latestRun ?? null
  const canStart = !running && !terminal

  const runAction = async (fn: () => Promise<unknown>, okMessage: string) => {
    setBusy(true)
    setActionError(null)
    try {
      await fn()
      toast(okMessage)
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const onStart = () =>
    runAction(
      () => (client.startIssue ? client.startIssue(taskId) : Promise.reject(new Error('未接入'))),
      '已交给 Agent',
    )

  const onCancel = () =>
    runAction(
      () => (client.cancelIssue ? client.cancelIssue(taskId, '用户取消') : Promise.reject(new Error('未接入'))),
      '已取消',
    )

  const onSend = (event: FormEvent) => {
    event.preventDefault()
    const content = draft.trim()
    if (!content || !client.sendMessage) return
    void runAction(async () => {
      await client.sendMessage!(taskId, content)
      setDraft('')
    }, '消息已送达 Agent')
  }

  return (
    <AppShell title={task.name} breadcrumb="Issue">
      <div className="cockpit-page">
        <header className="cockpit-header">
          <div>
            <div className="cockpit-title">
              <Link to="/issues">← Issue</Link>
              <span data-testid="task-status">
                <StateTag tone={STATUS_TONES[task.status]}>{STATUS_LABELS[task.status]}</StateTag>
              </span>
            </div>
            <div className="cockpit-submeta">
              <span className="mono">{task.id.slice(0, 8)}</span>
              <span>更新于 {formatTime(task.updatedAt)}</span>
              {task.planBranch ? <span className="mono">{task.planBranch}</span> : null}
              {detail.agents?.[0] ? <span>Agent · {detail.agents[0].name}</span> : null}
            </div>
          </div>
          <div className="button-row">
            {canStart ? (
              <button type="button" className="button primary" onClick={onStart} disabled={busy}>
                {task.status === 'pending' ? '开始执行' : '继续执行'}
              </button>
            ) : null}
            {running ? (
              <button type="button" className="button ghost" onClick={onCancel} disabled={busy}>
                取消
              </button>
            ) : null}
          </div>
        </header>

        <div className="cockpit-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'chat'}
            className={tab === 'chat' ? 'active' : ''}
            onClick={() => setTab('chat')}
          >
            会话
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'records'}
            className={tab === 'records' ? 'active' : ''}
            onClick={() => setTab('records')}
          >
            记录
          </button>
        </div>

        {tab === 'records' ? (
          <AuditTimeline events={stream.events} connected={stream.connected} />
        ) : (
          <div className="cockpit-shell">
            <div className="thread">
              <article className="msg msg-requirement" data-testid="thread-requirement">
                <div className="msg-head">
                  <span className="role-badge">需求</span>
                  <span className="msg-time">{formatTime(task.createdAt)}</span>
                </div>
                <div className="msg-body">{task.name}</div>
              </article>

              {thread.length === 0 ? (
                <div className="empty-inline">
                  {running ? 'Agent 正在执行，稍后会出现过程记录。' : '还没有对话，点「开始执行」或直接发一条消息。'}
                </div>
              ) : (
                thread.map((item) => <ThreadBubble key={item.key} item={item} />)
              )}

              {latestRun && latestRun.status === 'failed' && !running ? (
                <div className="thread-system failed">
                  <StateDot tone="failed" />
                  <span>上一轮执行失败：{latestRun.detail || latestRun.errorClass || '未知原因'}</span>
                </div>
              ) : null}

              {artifacts.length > 0 ? (
                <section className="artifact-panel" data-testid="artifacts">
                  <div className="artifact-panel-head">
                    <strong>产物</strong>
                    <span className="muted">{artifacts.length} 个文件</span>
                  </div>
                  <div className="artifact-list">
                    {artifacts.map((item) => {
                      const name = item.path.split('/').pop() ?? item.path
                      return (
                        <div className="artifact-card" key={item.id}>
                          <span className="artifact-icon">{fileBadge(item.path)}</span>
                          <div className="artifact-meta">
                            <span className="artifact-name">{name}</span>
                            <span className="artifact-sub">
                              {formatBytes(item.sizeBytes)}
                              {item.path !== name ? ` · ${item.path}` : ''}
                            </span>
                          </div>
                          <button type="button" className="artifact-action" onClick={() => void onDownload(item)}>
                            下载
                          </button>
                        </div>
                      )
                    })}
                  </div>
                </section>
              ) : null}

              {actionError ? <div className="notice danger">{actionError}</div> : null}
            </div>

            <form className="composer" onSubmit={onSend}>
              <textarea
                className="composer-input"
                aria-label="会话输入"
                placeholder={
                  running
                    ? 'Agent 正在执行，等本轮结束后再发消息'
                    : terminal
                      ? '任务已结束，不能再发消息'
                      : '继续交代后续工作…'
                }
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                disabled={running || terminal}
                rows={3}
              />
              <div className="composer-bar">
                <span className="composer-hint">
                  每条消息都会交给 Agent，形成一轮新执行。
                </span>
                <button
                  type="submit"
                  className="button primary send-btn"
                  disabled={running || terminal || !draft.trim() || busy}
                >
                  发送
                </button>
              </div>
            </form>
          </div>
        )}
      </div>
    </AppShell>
  )
}
