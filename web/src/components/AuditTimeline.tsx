import { useMemo, useState, type ReactNode } from 'react'

import type { StreamEvent } from '../api/issues'
import { StateDot, StateTag, type TagTone } from './ui'

/**
 * 审计时间线：按「一轮执行」分组展示里程碑、工具调用、用量与错误。
 *
 * 新模型没有步骤，`run_id` 就是分组键；不属于任何一轮的任务级事件
 * （创建、状态上报、取消）单独成组。
 */

const EVENT_LABELS: Record<string, string> = {
  'task.created': '任务已创建',
  'turn.started': '开始一轮执行',
  'turn.done': '本轮结束',
  'turn.failed': '本轮失败',
  'turn.cancelled': '本轮已取消',
  'issue.status': 'Issue 状态变更',
  'issue.input_requested': 'Agent 请求输入',
  'tool.call': '工具调用',
  'agent.notice': 'Agent 通知',
  'agent.narration': '执行过程',
  'message.user': '用户消息',
  'message.assistant': 'Agent 回复',
  'task.cancel_requested': '请求取消',
  'work_item.started': '工作项开始',
  'work_item.blocked': '工作项阻塞',
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

const STATUS_NAMES: Record<string, string> = {
  pending: '待开始',
  executing: '执行中',
  paused: '已阻塞',
  completed: '已完成',
  cancelled: '已取消',
  failed: '已失败',
}

function statusLabel(value: string): string {
  return STATUS_NAMES[value] ?? value
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function clipText(value: string, limit: number): string {
  const runes = Array.from(value)
  return runes.length <= limit ? value : `${runes.slice(0, limit).join('')}…`
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
  events: StreamEvent[]
}

/** 一条时间线条目：单个事件，或折叠后的连续同名工具调用。 */
type TimelineItem =
  | { kind: 'event'; key: string; event: StreamEvent }
  | {
      kind: 'tool-group'
      key: string
      title: string
      events: StreamEvent[]
      durationMs: number
    }

/** 连续出现同名工具调用时折叠，避免「Edit 同一个文件 ×20」铺满整屏。 */
function buildItems(events: StreamEvent[]): TimelineItem[] {
  const items: TimelineItem[] = []
  let run: StreamEvent[] = []

  const flush = () => {
    if (run.length === 0) return
    if (run.length < 3) {
      for (const event of run) {
        items.push({ kind: 'event', key: event.eventId, event })
      }
    } else {
      const duration = run.reduce((sum, event) => sum + num(payloadOf(event).duration_ms), 0)
      items.push({
        kind: 'tool-group',
        key: `group-${run[0].eventId}`,
        title: eventTitle(run[0]),
        events: run,
        durationMs: duration,
      })
    }
    run = []
  }

  for (const event of events) {
    const isSameTool =
      event.kind === 'tool.call' &&
      run.length > 0 &&
      eventTitle(event) === eventTitle(run[0])
    if (isSameTool) {
      run.push(event)
      continue
    }
    flush()
    if (event.kind === 'tool.call') {
      run = [event]
    } else {
      items.push({ kind: 'event', key: event.eventId, event })
    }
  }
  flush()
  return items
}

function buildGroups(events: StreamEvent[]): Group[] {
  const byRun = new Map<string, StreamEvent[]>()
  const topLevel: StreamEvent[] = []
  for (const event of events) {
    const runId = text(payloadOf(event).run_id)
    if (runId) {
      const list = byRun.get(runId) ?? []
      list.push(event)
      byRun.set(runId, list)
    } else {
      topLevel.push(event)
    }
  }

  const groups: Group[] = []
  let index = 0
  for (const [runId, list] of byRun) {
    index += 1
    groups.push({ key: runId, title: `第 ${index} 轮执行`, events: list })
  }
  if (topLevel.length > 0) {
    // 任务级事件（创建、状态上报）放在最前面
    groups.unshift({ key: '__top__', title: '任务', events: topLevel })
  }
  return groups
}

function eventTone(event: StreamEvent): TagTone {
  const payload = payloadOf(event)
  if (event.kind === 'turn.failed') return 'failed'
  if (event.kind === 'turn.done') return 'done'
  if (event.kind === 'turn.cancelled') return 'cancelled'
  if (event.kind === 'tool.call') {
    const status = text(payload.status)
    if (status === 'failed') return 'failed'
    if (status === 'completed') return 'done'
    return 'running'
  }
  if (event.kind === 'issue.status') {
    const to = text(payload.to)
    if (to === 'cancelled') return 'cancelled'
    if (to === 'failed') return 'failed'
    if (to === 'completed') return 'done'
    return 'review'
  }
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
      const detail = text(payload.detail)
      // Bash 的标题就是命令本身，别再重复一行。
      const showDetail = detail !== '' && detail !== text(payload.title)
      return (
        <>
          {showDetail ? (
            <code className="audit-tool-detail" title={detail}>
              {detail}
            </code>
          ) : null}
          <span>{parts.join(' · ')}</span>
        </>
      )
    }
    case 'turn.failed': {
      const reason = text(payload.reason)
      const errorClass = text(payload.error_class)
      return (
        <>
          {errorClass ? <span className="audit-chip danger">{errorClass}</span> : null}
          {reason ? <span className="audit-reason">{reason}</span> : null}
        </>
      )
    }
    case 'turn.done': {
      const input = num(payload.input_tokens)
      const output = num(payload.output_tokens)
      const duration = num(payload.duration_ms)
      const commit = text(payload.commit_sha)
      const pr = text(payload.pr_url)
      return (
        <>
          {input || output ? (
            <span className="audit-chip">
              tokens {input} / {output}
            </span>
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
    case 'issue.status': {
      const from = text(payload.from)
      const to = text(payload.to)
      const reason = text(payload.reason)
      return (
        <>
          {from && to ? (
            <span className="audit-chip">
              {statusLabel(from)} → {statusLabel(to)}
            </span>
          ) : null}
          {reason ? <span className="audit-note">{reason}</span> : null}
        </>
      )
    }
    case 'task.created':
      return text(payload.mode) ? `模式 ${text(payload.mode)}` : ''
    case 'agent.notice':
      return text(payload.text)
    case 'agent.narration':
      return <span className="audit-note">{clipText(text(payload.text), 140)}</span>
    default:
      return ''
  }
}

function matchesFilter(event: StreamEvent, filter: Filter): boolean {
  if (filter === 'all') return true
  if (filter === 'tools') return event.kind === 'tool.call'
  return eventTone(event) === 'failed'
}

export interface AuditTimelineProps {
  events: StreamEvent[]
  connected: boolean
}

export default function AuditTimeline({ events, connected }: AuditTimelineProps) {
  const [filter, setFilter] = useState<Filter>('all')
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})

  const groups = useMemo(() => buildGroups(events), [events])

  const visibleGroups = useMemo(
    () =>
      groups
        .map((group) => ({
          ...group,
          events: group.events.filter((event) => matchesFilter(event, filter)),
        }))
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
            const isCollapsed = collapsed[group.key] === true
            const toolCount = group.events.filter((event) => event.kind === 'tool.call').length
            const failed = group.events.some((event) => eventTone(event) === 'failed')
            const tone: TagTone = failed ? 'failed' : 'muted'
            return (
              <section className={`audit-group ${tone}`} key={group.key}>
                <button
                  type="button"
                  className="audit-group-head"
                  aria-expanded={!isCollapsed}
                  onClick={() =>
                    setCollapsed((prev) => ({ ...prev, [group.key]: !isCollapsed }))
                  }
                >
                  <StateDot tone={tone} />
                  <strong>{group.title}</strong>
                  <StateTag tone={tone}>{failed ? '有错误' : '记录'}</StateTag>
                  {toolCount > 0 ? (
                    <span className="audit-meta">{toolCount} 次工具调用</span>
                  ) : null}
                  <span className={`audit-chevron ${isCollapsed ? '' : 'expanded'}`} aria-hidden>
                    ›
                  </span>
                </button>

                {isCollapsed ? null : (
                  <ol className="audit-items">
                    {buildItems(group.events).map((item) => {
                      if (item.kind === 'tool-group') {
                        return (
                          <li
                            className="audit-item done audit-item-group"
                            key={item.key}
                            data-testid={`event-${item.events[0].seq}`}
                          >
                            <span className="audit-dot" aria-hidden />
                            <div className="audit-item-main">
                              <div className="audit-item-title">
                                <span className="audit-item-name">{item.title}</span>
                                <span className="audit-group-count">×{item.events.length}</span>
                                <span className="audit-item-time">
                                  {formatTime(item.events[0].createdAt)} –{' '}
                                  {formatTime(item.events[item.events.length - 1].createdAt)}
                                </span>
                              </div>
                              <div className="audit-item-detail">
                                连续 {item.events.length} 次 · 共 {formatDuration(item.durationMs)}
                              </div>
                              <details className="audit-raw">
                                <summary>展开每一次</summary>
                                <ol className="audit-subitems">
                                  {item.events.map((event) => (
                                    <li key={event.eventId}>
                                      <span className="audit-item-time">{formatTime(event.createdAt)}</span>
                                      <span>{eventDetail(event) || '成功'}</span>
                                    </li>
                                  ))}
                                </ol>
                              </details>
                            </div>
                          </li>
                        )
                      }
                      const event = item.event
                      const itemTone = eventTone(event)
                      return (
                        <li
                          className={`audit-item ${itemTone}`}
                          key={item.key}
                          data-testid={`event-${event.seq}`}
                        >
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
