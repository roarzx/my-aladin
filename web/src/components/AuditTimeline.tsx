import { useMemo, useState, type ReactNode } from 'react'

import type { PlanStep, StreamEvent } from '../api/issues'
import { StateDot, StateTag, type TagTone } from './ui'

/** 审计时间线：按步骤分组展示里程碑、工具调用、用量与错误。 */

const EVENT_LABELS: Record<string, string> = {
  'task.created': '任务已创建',
  'plan.submitted': '计划已提交',
  'plan.confirmed': '计划已确认',
  'step.dispatched': '步骤已派发',
  'step.done': '步骤已完成',
  'step.fail': '步骤失败',
  'step.retried': '步骤已重试',
  'step.reconciling': '步骤转对账',
  'tool.call': '工具调用',
  'agent.notice': 'Agent 通知',
  'task.cancel_requested': '请求取消',
  'task.cancelled': '任务已取消',
  'execution.orphaned': '执行失联',
  'work_item.started': '工作项开始',
  'work_item.blocked': '工作项阻塞',
}

const STEP_STATUS_META: Record<string, { label: string; tone: TagTone }> = {
  pending: { label: '待执行', tone: 'pending' },
  executing: { label: '执行中', tone: 'running' },
  reconciling: { label: '对账中', tone: 'review' },
  completed: { label: '已完成', tone: 'done' },
  failed: { label: '失败', tone: 'failed' },
  cancelled: { label: '已取消', tone: 'cancelled' },
  skipped: { label: '已跳过', tone: 'muted' },
}

type Filter = 'all' | 'tools' | 'errors'

const FILTERS: Array<{ key: Filter; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'tools', label: '工具调用' },
  { key: 'errors', label: '错误' },
]

function payloadOf(event: StreamEvent): Record<string, unknown> {
  const payload = event.payload
  if (payload && typeof payload === 'object' && !Array.isArray(payload)) {
    return payload as Record<string, unknown>
  }
  return {}
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString('zh-CN', { hour12: false })
}

function formatDuration(ms: number): string {
  if (!ms || ms < 0) return ''
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m${String(Math.round(seconds % 60)).padStart(2, '0')}s`
}

interface Group {
  key: string
  title: string
  ordinal: number | null
  step?: PlanStep
  events: StreamEvent[]
}

function buildGroups(events: StreamEvent[], steps: PlanStep[]): Group[] {
  const byStep = new Map<string, StreamEvent[]>()
  const taskEvents: StreamEvent[] = []
  for (const event of events) {
    const stepId = text(payloadOf(event).step_id)
    if (stepId) {
      const list = byStep.get(stepId) ?? []
      list.push(event)
      byStep.set(stepId, list)
    } else {
      taskEvents.push(event)
    }
  }

  const groups: Group[] = []
  if (taskEvents.length > 0) {
    groups.push({ key: '__task__', title: '任务', ordinal: null, events: taskEvents })
  }
  const known = new Set<string>()
  for (const step of steps) {
    known.add(step.id)
    const list = byStep.get(step.id)
    if (list && list.length > 0) {
      groups.push({
        key: step.id,
        title: `${step.ordinal}. ${step.title}`,
        ordinal: step.ordinal,
        step,
        events: list,
      })
    }
  }
  for (const [stepId, list] of byStep) {
    if (known.has(stepId)) continue
    groups.push({
      key: stepId,
      title: `步骤 ${stepId.slice(0, 8)}`,
      ordinal: null,
      events: list,
    })
  }
  return groups
}

function groupStatus(group: Group): { label: string; tone: TagTone; running: boolean } {
  if (group.step) {
    const meta = STEP_STATUS_META[group.step.status] ?? { label: group.step.status, tone: 'muted' as TagTone }
    return { label: meta.label, tone: meta.tone, running: group.step.status === 'executing' }
  }
  const failed = group.events.some((event) => event.kind === 'step.fail' || event.kind === 'tool.call' && text(payloadOf(event).status) === 'failed')
  if (failed) return { label: '有错误', tone: 'failed', running: false }
  return { label: '记录', tone: 'muted', running: false }
}

function groupDuration(group: Group): number {
  const dispatched = group.events.find((event) => event.kind === 'step.dispatched')
  const finished = group.events.find(
    (event) => event.kind === 'step.done' || event.kind === 'step.fail',
  )
  if (!dispatched || !finished) return 0
  const start = new Date(dispatched.createdAt).getTime()
  const end = new Date(finished.createdAt).getTime()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return 0
  return end - start
}

function eventTone(event: StreamEvent): TagTone {
  const payload = payloadOf(event)
  if (event.kind === 'step.fail') return 'failed'
  if (event.kind === 'step.reconciling') return 'review'
  if (event.kind === 'step.done') return 'done'
  if (event.kind === 'tool.call') {
    const status = text(payload.status)
    if (status === 'failed') return 'failed'
    if (status === 'completed') return 'done'
    return 'running'
  }
  if (event.kind === 'task.cancelled') return 'cancelled'
  if (event.kind === 'agent.notice') return 'paused'
  return 'muted'
}

function eventTitle(event: StreamEvent): string {
  const payload = payloadOf(event)
  if (event.kind === 'tool.call') {
    return text(payload.title) || text(payload.tool_kind) || '工具调用'
  }
  return EVENT_LABELS[event.kind] ?? event.kind
}

function eventDetail(event: StreamEvent): ReactNode {
  const payload = payloadOf(event)
  switch (event.kind) {
    case 'tool.call': {
      const parts: string[] = []
      const kind = text(payload.tool_kind)
      if (kind) parts.push(kind)
      const duration = num(payload.duration_ms)
      if (duration > 0) parts.push(formatDuration(duration))
      const status = text(payload.status)
      if (status) parts.push(status === 'failed' ? '失败' : status === 'completed' ? '成功' : status)
      return parts.join(' · ')
    }
    case 'step.fail': {
      const reason = text(payload.reason)
      const errorClass = text(payload.error_class)
      return (
        <>
          {errorClass ? <span className="audit-chip danger">{errorClass}</span> : null}
          {reason ? <span className="audit-reason">{reason}</span> : null}
        </>
      )
    }
    case 'step.done': {
      const input = num(payload.input_tokens)
      const output = num(payload.output_tokens)
      const duration = num(payload.duration_ms)
      const commit = text(payload.commit_sha)
      const pr = text(payload.pr_url)
      return (
        <>
          {input || output ? (
            <span className="audit-chip">tokens {input} / {output}</span>
          ) : null}
          {duration ? <span className="audit-chip">{formatDuration(duration)}</span> : null}
          {commit ? <span className="mono-snippet">commit {commit.slice(0, 8)}</span> : null}
          {pr ? (
            <a className="timeline-pr" href={pr} target="_blank" rel="noreferrer noopener">
              PR
            </a>
          ) : null}
        </>
      )
    }
    case 'step.dispatched':
      return text(payload.attempt_no) ? `第 ${text(payload.attempt_no)} 次尝试` : ''
    case 'step.retried':
      return text(payload.attempt_no) ? `第 ${text(payload.attempt_no)} 次尝试` : ''
    case 'plan.submitted':
      return text(payload.revision) ? `r${text(payload.revision)}` : ''
    case 'task.created':
      return text(payload.mode) ? `模式 ${text(payload.mode)}` : ''
    case 'agent.notice':
      return text(payload.text)
    default:
      return ''
  }
}

function matchesFilter(event: StreamEvent, filter: Filter): boolean {
  if (filter === 'all') return true
  if (filter === 'tools') return event.kind === 'tool.call'
  const tone = eventTone(event)
  return tone === 'failed' || event.kind === 'step.reconciling'
}

export interface AuditTimelineProps {
  events: StreamEvent[]
  steps: PlanStep[]
  connected: boolean
}

export default function AuditTimeline({ events, steps, connected }: AuditTimelineProps) {
  const [filter, setFilter] = useState<Filter>('all')
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})

  const groups = useMemo(() => buildGroups(events, steps), [events, steps])

  const visibleGroups = useMemo(
    () =>
      groups
        .map((group) => ({ ...group, events: group.events.filter((event) => matchesFilter(event, filter)) }))
        .filter((group) => group.events.length > 0),
    [groups, filter],
  )

  const totalTools = events.filter((event) => event.kind === 'tool.call').length
  const totalErrors = events.filter((event) => eventTone(event) === 'failed').length

  return (
    <div className="audit">
      <div className="audit-head">
        <div className="filters" role="group" aria-label="记录筛选">
          {FILTERS.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`filter${filter === item.key ? ' active' : ''}`}
              onClick={() => setFilter(item.key)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <div className="audit-summary">
          <span>{steps.length} 个步骤</span>
          <span>{totalTools} 次工具调用</span>
          {totalErrors > 0 ? <span className="error">{totalErrors} 个错误</span> : null}
          <span className="stream-state">
            <StateDot tone={connected ? 'running' : 'muted'} />
            {connected ? '实时更新中' : '连接中断，正在重连'}
          </span>
        </div>
      </div>

      {visibleGroups.length === 0 ? (
        <div className="empty-inline">{events.length ? '没有匹配的记录' : '暂无事件'}</div>
      ) : (
        <div className="audit-groups">
          {visibleGroups.map((group) => {
            const status = groupStatus(group)
            const duration = formatDuration(groupDuration(group))
            const isCollapsed = collapsed[group.key] === true
            const toolCount = group.events.filter((event) => event.kind === 'tool.call').length
            return (
              <section className={`audit-group ${status.tone}`} key={group.key}>
                <button
                  type="button"
                  className="audit-group-head"
                  aria-expanded={!isCollapsed}
                  onClick={() =>
                    setCollapsed((prev) => ({ ...prev, [group.key]: !isCollapsed }))
                  }
                >
                  <StateDot tone={status.tone} />
                  <strong>{group.title}</strong>
                  <StateTag tone={status.tone}>{status.label}</StateTag>
                  {duration ? <span className="audit-meta">{duration}</span> : null}
                  {toolCount > 0 ? <span className="audit-meta">{toolCount} 次工具调用</span> : null}
                  <span className={`audit-chevron ${isCollapsed ? '' : 'expanded'}`} aria-hidden>
                    ›
                  </span>
                </button>

                {isCollapsed ? null : (
                  <ol className="audit-items">
                    {group.events.map((event) => {
                      const tone = eventTone(event)
                      return (
                        <li className={`audit-item ${tone}`} key={event.eventId} data-testid={`event-${event.seq}`}>
                          <span className="audit-dot" aria-hidden />
                          <div className="audit-item-main">
                            <div className="audit-item-title">
                              <span className="audit-item-name">{eventTitle(event)}</span>
                              <span className="audit-item-time">{formatTime(event.createdAt)}</span>
                            </div>
                            <div className="audit-item-detail">{eventDetail(event)}</div>
                            <details className="audit-raw">
                              <summary>原始数据</summary>
                              <pre>{JSON.stringify(event.payload, null, 2)}</pre>
                            </details>
                          </div>
                        </li>
                      )
                    })}
                  </ol>
                )}
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}
