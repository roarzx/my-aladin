import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  fetchEvents,
  fetchIssue,
  fetchPlan,
  retryStep,
  type EventPage,
  type IssueDetail,
  type PlanDetail,
  type PlanStep,
  type TaskStatus,
} from '../api/issues'
import { useEventStream } from '../hooks/useEventStream'

export interface CockpitApi {
  fetchIssue: typeof fetchIssue
  fetchPlan: typeof fetchPlan
  retryStep: typeof retryStep
}

const defaultApi: CockpitApi = { fetchIssue, fetchPlan, retryStep }

export interface IssueCockpitProps {
  taskId: string
  api?: CockpitApi
  /** 可注入的事件拉取函数（测试用）；默认走 HTTP 轮询。 */
  eventsFetcher?: (taskId: string, after: number) => Promise<EventPage>
}

const STATUS_LABELS: Record<TaskStatus, string> = {
  pending: '待规划',
  plan_confirming: '待确认计划',
  executing: '执行中',
  paused: '已暂停',
  cancelling: '取消中',
  chatting: '对话中',
  cancelled: '已取消',
  failed: '已失败',
}

/** 失败恢复入口：仅 failed 步骤可重试；reconciling 步骤等待对账（§5.7.3）。 */
export function canRetry(step: PlanStep): boolean {
  return step.status === 'failed'
}

export default function IssueCockpit({
  taskId,
  api = defaultApi,
  eventsFetcher = fetchEvents,
}: IssueCockpitProps) {
  const [detail, setDetail] = useState<IssueDetail | null>(null)
  const [planDetail, setPlanDetail] = useState<PlanDetail | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [retrying, setRetrying] = useState<string | null>(null)

  const { events, connected, error: streamError } = useEventStream(taskId, 0, {
    fetcher: eventsFetcher,
  })

  const reload = useCallback(async () => {
    try {
      const next = await api.fetchIssue(taskId)
      setDetail(next)
      setLoadError(null)
      if (next.plan) {
        setPlanDetail(await api.fetchPlan(taskId, next.plan.revision))
      } else {
        setPlanDetail(null)
      }
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err))
    }
  }, [api, taskId])

  useEffect(() => {
    void reload()
  }, [reload])

  // 事件驱动刷新：新事件到达后重载权威状态（本地不缓存权威状态，§5.7.4）
  useEffect(() => {
    if (events.length > 0) void reload()
  }, [events.length, reload])

  const onRetry = useCallback(
    async (step: PlanStep) => {
      setRetrying(step.id)
      setActionError(null)
      try {
        await api.retryStep(step.id, '', step.version)
        await reload()
      } catch (err) {
        setActionError(err instanceof Error ? err.message : String(err))
      } finally {
        setRetrying(null)
      }
    },
    [api, reload],
  )

  const failedSteps = useMemo(
    () => (planDetail?.steps ?? []).filter((s) => s.status === 'failed'),
    [planDetail],
  )

  if (loadError && !detail) {
    return (
      <main className="cockpit" aria-label="Issue 驾驶舱">
        <p role="alert">加载失败：{loadError}</p>
      </main>
    )
  }

  if (!detail) {
    return (
      <main className="cockpit" aria-label="Issue 驾驶舱">
        <p>加载中…</p>
      </main>
    )
  }

  const { task } = detail
  const steps = planDetail?.steps ?? []

  return (
    <main className="cockpit" aria-label="Issue 驾驶舱">
      <header>
        <h1>{task.name}</h1>
        <p>
          状态：<strong data-testid="task-status">{STATUS_LABELS[task.status] ?? task.status}</strong>
          {task.planBranch ? <span> · 分支 {task.planBranch}</span> : null}
        </p>
        <p data-testid="stream-state">{connected ? '实时更新中' : '连接中断，正在重连'}</p>
        {streamError ? <p role="alert">事件流错误：{streamError.message}</p> : null}
      </header>

      <section aria-labelledby="plan-heading">
        <h2 id="plan-heading">计划</h2>
        {detail.plan ? (
          <p>
            版本 r{detail.plan.revision} · {detail.plan.status} · {detail.plan.stepCount} 步
          </p>
        ) : (
          <p>尚未提交计划</p>
        )}
        <ol>
          {steps.map((step) => (
            <li key={step.id} data-testid={`step-${step.ordinal}`}>
              <span>
                {step.ordinal}. {step.title}
              </span>{' '}
              <span data-testid={`step-status-${step.ordinal}`}>{step.status}</span>{' '}
              <span>@{step.agentRef}</span>
              {canRetry(step) ? (
                <button
                  type="button"
                  disabled={retrying === step.id}
                  onClick={() => void onRetry(step)}
                >
                  {retrying === step.id ? '重试中…' : '重试'}
                </button>
              ) : null}
            </li>
          ))}
        </ol>
        {actionError ? <p role="alert">操作失败：{actionError}</p> : null}
      </section>

      <section aria-labelledby="recovery-heading">
        <h2 id="recovery-heading">恢复</h2>
        {failedSteps.length === 0 ? (
          <p>无失败步骤</p>
        ) : (
          <ul>
            {failedSteps.map((step) => (
              <li key={step.id}>
                失败步骤 {step.ordinal}：{step.title}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-labelledby="timeline-heading">
        <h2 id="timeline-heading">执行时间线</h2>
        {events.length === 0 ? (
          <p>暂无事件</p>
        ) : (
          <ul aria-label="执行时间线">
            {events.map((event) => (
              <li key={event.eventId} data-testid={`event-${event.seq}`}>
                #{event.seq} {event.kind}
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  )
}
