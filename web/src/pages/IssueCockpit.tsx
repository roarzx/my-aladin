import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'

import {
  confirmPlan,
  fetchEvents,
  fetchIssue,
  fetchPlan,
  retryStep,
  startTask,
  submitPlan,
  type EventPage,
  type IssueDetail,
  type PlanDetail,
  type PlanStep,
  type TaskStatus,
} from '../api/issues'
import { clearTaskDraft, readTaskDraft } from '../api/taskDraft'
import AppShell from '../components/AppShell'
import { EmptyState, StateDot, StateTag, type TagTone } from '../components/ui'
import { useEventStream } from '../hooks/useEventStream'
import { Link } from '../router'
import '../styles/pages-issues.css'

export interface CockpitApi {
  fetchIssue: typeof fetchIssue
  fetchPlan: typeof fetchPlan
  retryStep: typeof retryStep
  /** 提交计划（POST /issues/{id}/plans）；测试注入的 api 可能不提供，故为可选。 */
  submitPlan?: typeof submitPlan
  /** 确认计划（POST /plans/{id}/confirm）。 */
  confirmPlan?: typeof confirmPlan
  /** 启动任务（POST /issues/{id}/start）。 */
  startTask?: typeof startTask
}

const defaultApi: CockpitApi = {
  fetchIssue,
  fetchPlan,
  retryStep,
  submitPlan,
  confirmPlan,
  startTask,
}

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

const STATUS_TONES: Record<TaskStatus, TagTone> = {
  pending: 'pending',
  plan_confirming: 'review',
  executing: 'running',
  paused: 'input',
  cancelling: 'queued',
  chatting: 'review',
  cancelled: 'cancelled',
  failed: 'failed',
}

/** 失败恢复入口：仅 failed 步骤可重试；reconciling 步骤等待对账（§5.7.3）。 */
export function canRetry(step: PlanStep): boolean {
  return step.status === 'failed'
}

/** 步骤状态的中文标签与色调（对照阿拉丁状态体系）。 */
const STEP_STATUS_META: Record<string, { label: string; tone: TagTone }> = {
  pending: { label: '待执行', tone: 'pending' },
  queued: { label: '待执行', tone: 'queued' },
  dispatched: { label: '已派发', tone: 'queued' },
  running: { label: '执行中', tone: 'running' },
  reconciling: { label: '对账中', tone: 'review' },
  completed: { label: '已完成', tone: 'done' },
  failed: { label: '失败', tone: 'failed' },
  skipped: { label: '已跳过', tone: 'muted' },
  cancelled: { label: '已取消', tone: 'cancelled' },
}

function stepStatusMeta(status: string): { label: string; tone: TagTone } {
  return STEP_STATUS_META[status] ?? { label: status, tone: 'muted' }
}

/** 计划状态中文映射，用于「版本 rN · 状态 · N 步」一行。 */
const PLAN_STATUS_LABELS: Record<string, string> = {
  idle: '待确认',
  draft: '草稿',
  submitted: '待确认',
  confirmed: '已确认',
  executing: '执行中',
  paused: '已暂停',
  stopped: '已停止',
  completed: '已完成',
  superseded: '已被取代',
  cancelled: '已取消',
}

/** 时间线事件 payload 的可读摘要；空对象不渲染，避免与事件类型重复。 */
function eventPayloadText(payload: unknown): string {
  if (payload === null || typeof payload !== 'object') return ''
  if (Array.isArray(payload) && payload.length === 0) return ''
  if (!Array.isArray(payload) && Object.keys(payload as Record<string, unknown>).length === 0) return ''
  try {
    return JSON.stringify(payload)
  } catch {
    return ''
  }
}

/** 从 step.done 事件 payload 中取出 PR 链接（兼容 snake_case 与 camelCase）。 */
function eventPrUrl(payload: unknown): string {
  if (payload === null || typeof payload !== 'object') return ''
  const record = payload as Record<string, unknown>
  const url = record.pr_url ?? record.prUrl
  return typeof url === 'string' ? url : ''
}

interface StepDraft {
  title: string
  body: string
  agentRef: string
}

function emptyStepDraft(): StepDraft {
  return { title: '', body: '', agentRef: 'a1' }
}

/** 步骤卡片的状态色，仅用于呈现；原始状态文本照常展示。 */
function stepCardClass(status: string): string {
  if (status === 'completed') return 'step-card done'
  if (status === 'failed') return 'step-card failed'
  if (status === 'running' || status === 'dispatched' || status === 'reconciling') {
    return 'step-card active'
  }
  return 'step-card'
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

  // 提交计划表单：正文默认取自创建任务时暂存在浏览器侧的描述。
  const [planContent, setPlanContent] = useState('')
  const [planSteps, setPlanSteps] = useState<StepDraft[]>(() => [emptyStepDraft()])
  const [planNotice, setPlanNotice] = useState('')
  const [planBusy, setPlanBusy] = useState<'submit' | 'confirm' | 'start' | null>(null)

  useEffect(() => {
    if (!taskId) return
    setPlanContent((prev) => prev || readTaskDraft(taskId))
  }, [taskId])

  // 空 taskId 时不应发起轮询，直接返回空页，避免无意义请求。
  const fetchStream = useCallback(
    (id: string, after: number): Promise<EventPage> =>
      id ? eventsFetcher(id, after) : Promise.resolve({ events: [], nextSeq: after }),
    [eventsFetcher],
  )

  const { events, connected, error: streamError } = useEventStream(taskId, 0, {
    fetcher: fetchStream,
  })

  const reload = useCallback(async () => {
    if (!taskId) return
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

  if (!taskId) {
    return (
      <AppShell
        title="Issue 驾驶舱"
        breadcrumb="我的 Issue / Issue 驾驶舱"
        className="cockpit-page"
      >
        <EmptyState
          title="未指定任务"
          description="请从 Issue 列表选择一个任务，再进入驾驶舱查看计划与执行时间线。"
          action={
            <Link className="button primary" to="/task/list">
              返回 Issue 列表
            </Link>
          }
        />
      </AppShell>
    )
  }

  if (loadError && !detail) {
    return (
      <AppShell
        title="Issue 驾驶舱"
        breadcrumb="我的 Issue / Issue 驾驶舱"
        className="cockpit-page"
      >
        <p role="alert" className="error">
          加载失败：{loadError}
        </p>
        <div className="button-row">
          <button
            type="button"
            className="button ghost"
            onClick={() => {
              localStorage.removeItem('aladdin_token')
              window.location.reload()
            }}
          >
            重新设置令牌
          </button>
          <Link className="button ghost" to="/task/list">
            ← 任务列表
          </Link>
        </div>
      </AppShell>
    )
  }

  if (!detail) {
    return (
      <AppShell
        title="Issue 驾驶舱"
        breadcrumb="我的 Issue / Issue 驾驶舱"
        className="cockpit-page"
      >
        <section className="panel">
          <div className="empty-inline">加载中…</div>
        </section>
      </AppShell>
    )
  }

  const { task } = detail
  const steps = planDetail?.steps ?? []
  const statusLabel = STATUS_LABELS[task.status] ?? task.status
  const statusTone = STATUS_TONES[task.status] ?? 'muted'
  // 计划摘要与计划详情可能先后到达，优先用更完整的详情。
  const activePlan = planDetail?.plan ?? detail.plan
  const planIdle = activePlan?.status === 'idle'

  const updateStepDraft = (index: number, patch: Partial<StepDraft>) => {
    setPlanSteps((prev) => prev.map((item, i) => (i === index ? { ...item, ...patch } : item)))
  }

  const addStepDraft = () => {
    setPlanSteps((prev) => [...prev, { ...emptyStepDraft(), agentRef: prev[prev.length - 1]?.agentRef || 'a1' }])
  }

  const removeStepDraft = (index: number) => {
    setPlanSteps((prev) => (prev.length <= 1 ? prev : prev.filter((_, i) => i !== index)))
  }

  const submitProposal = async (event: FormEvent) => {
    event.preventDefault()
    const content = planContent.trim()
    if (!content) {
      setPlanNotice('请输入计划正文。')
      return
    }
    if (planSteps.length === 0) {
      setPlanNotice('至少添加 1 个步骤。')
      return
    }
    for (let i = 0; i < planSteps.length; i += 1) {
      const item = planSteps[i]
      if (!item.title.trim()) {
        setPlanNotice(`第 ${i + 1} 步需要填写标题。`)
        return
      }
      if (!item.agentRef.trim()) {
        setPlanNotice(`第 ${i + 1} 步需要填写 agentRef（默认 a1）。`)
        return
      }
    }
    const submit = api.submitPlan
    if (!submit) {
      setPlanNotice('当前环境不支持提交计划。')
      return
    }

    setPlanBusy('submit')
    setPlanNotice('')
    setActionError(null)
    try {
      await submit(taskId, {
        contentMd: content,
        steps: planSteps.map((item, i) => ({
          ordinal: i + 1,
          title: item.title.trim(),
          body: item.body.trim(),
          agentRef: item.agentRef.trim(),
        })),
      })
      clearTaskDraft(taskId)
      await reload()
    } catch (err) {
      setPlanNotice(err instanceof Error ? err.message : String(err))
    } finally {
      setPlanBusy(null)
    }
  }

  const confirmOnly = async () => {
    const confirm = api.confirmPlan
    if (!activePlan || !confirm) return
    setPlanBusy('confirm')
    setActionError(null)
    try {
      await confirm(activePlan.id, activePlan.revision, activePlan.contentHash)
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setPlanBusy(null)
    }
  }

  const confirmAndStart = async () => {
    const confirm = api.confirmPlan
    const start = api.startTask
    if (!activePlan || !confirm || !start) {
      setActionError('当前环境不支持启动任务。')
      return
    }
    setPlanBusy('start')
    setActionError(null)
    try {
      await confirm(activePlan.id, activePlan.revision, activePlan.contentHash)
      await start(taskId, detail.task.version)
      await reload()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setPlanBusy(null)
    }
  }

  return (
    <AppShell
      title="Issue 驾驶舱"
      breadcrumb="我的 Issue / Issue 驾驶舱"
      className="cockpit-page"
    >
      <div className="cockpit-header">
        <div>
          <div className="cockpit-header-title-row">
            <h2>{task.name}</h2>
            <StateTag tone={statusTone}>{statusLabel}</StateTag>
          </div>
          <div className="cockpit-header-meta">
            <span>
              状态：
              <strong data-testid="task-status">{statusLabel}</strong>
            </span>
            {task.planBranch ? <span>分支 {task.planBranch}</span> : null}
            <span>工作区 {task.workspaceId}</span>
          </div>
        </div>
      </div>

      {streamError ? (
        <p role="alert" className="notice danger">
          事件流错误：{streamError.message}
        </p>
      ) : null}

      <div className="cockpit-grid">
        <section className="panel steps-panel" aria-labelledby="plan-heading">
          <div className="panel-head">
            <div>
              <h2 id="plan-heading">计划</h2>
              <p>
                {activePlan
                  ? `版本 r${activePlan.revision} · ${PLAN_STATUS_LABELS[activePlan.status] ?? activePlan.status} · ${detail.plan?.stepCount ?? steps.length} 步`
                  : '尚未提交计划'}
              </p>
            </div>
            {planIdle && activePlan ? (
              <div className="panel-actions">
                <button
                  type="button"
                  className="button ghost small"
                  disabled={planBusy !== null || !api.confirmPlan}
                  onClick={() => void confirmOnly()}
                >
                  {planBusy === 'confirm' ? '确认中…' : '确认计划'}
                </button>
                <button
                  type="button"
                  className="button primary small"
                  disabled={planBusy !== null || !api.confirmPlan || !api.startTask}
                  onClick={() => void confirmAndStart()}
                >
                  {planBusy === 'start' ? '启动中…' : '确认并启动'}
                </button>
              </div>
            ) : null}
          </div>

          {!detail.plan ? (
            <form className="plan-form" onSubmit={(event) => void submitProposal(event)}>
              <label className="field wide">
                <span>计划正文（Markdown）</span>
                <textarea
                  className="system-prompt-input"
                  rows={8}
                  value={planContent}
                  onChange={(event) => {
                    setPlanContent(event.target.value)
                    if (planNotice) setPlanNotice('')
                  }}
                  placeholder="描述任务目标、背景与验收标准。默认取自创建任务时填写的需求描述。"
                />
              </label>

              <div className="plan-steps-editor">
                {planSteps.map((item, index) => (
                  <div className="plan-step-row" key={index} data-testid={`plan-step-${index + 1}`}>
                    <div className="plan-step-index">{index + 1}</div>
                    <div className="plan-step-fields">
                      <input
                        value={item.title}
                        placeholder="步骤标题，例如：定位并修复缺陷"
                        aria-label={`第 ${index + 1} 步标题`}
                        onChange={(event) => updateStepDraft(index, { title: event.target.value })}
                      />
                      <textarea
                        rows={2}
                        value={item.body}
                        placeholder="步骤说明（可选）"
                        aria-label={`第 ${index + 1} 步说明`}
                        onChange={(event) => updateStepDraft(index, { body: event.target.value })}
                      />
                      <input
                        className="plan-step-agent"
                        value={item.agentRef}
                        placeholder="agentRef，默认 a1"
                        aria-label={`第 ${index + 1} 步 agentRef`}
                        onChange={(event) => updateStepDraft(index, { agentRef: event.target.value })}
                      />
                    </div>
                    <button
                      type="button"
                      className="button ghost small"
                      disabled={planSteps.length <= 1}
                      onClick={() => removeStepDraft(index)}
                    >
                      删除
                    </button>
                  </div>
                ))}
                <button type="button" className="button ghost" onClick={addStepDraft}>
                  添加步骤
                </button>
              </div>

              {planNotice ? (
                <p role="alert" className="notice danger">
                  {planNotice}
                </p>
              ) : null}

              <div className="form-actions">
                <button type="submit" className="button primary" disabled={planBusy !== null}>
                  {planBusy === 'submit' ? '提交中…' : '提交计划'}
                </button>
              </div>
            </form>
          ) : null}

          <ol className="step-list">
            {steps.map((step) => (
              <li
                key={step.id}
                className={stepCardClass(step.status)}
                data-testid={`step-${step.ordinal}`}
              >
                <div className="step-top">
                  <span className="step-title">
                    {step.ordinal}. {step.title}
                  </span>
                  <span
                    className="step-status"
                    data-testid={`step-status-${step.ordinal}`}
                  >
                    {(() => {
                      const meta = stepStatusMeta(step.status)
                      return <StateTag tone={meta.tone}>{meta.label}</StateTag>
                    })()}
                  </span>
                </div>
                <div className="step-meta">
                  <span>@{step.agentRef}</span>
                  {canRetry(step) ? (
                    <button
                      type="button"
                      className="button ghost small"
                      disabled={retrying === step.id}
                      onClick={() => void onRetry(step)}
                    >
                      {retrying === step.id ? '重试中…' : '重试'}
                    </button>
                  ) : null}
                </div>
              </li>
            ))}
          </ol>

          {actionError ? (
            <p role="alert" className="error">
              操作失败：{actionError}
            </p>
          ) : null}
        </section>

        <div className="cockpit-column">
          <section className="panel recovery-panel" aria-labelledby="recovery-heading">
            <div className="panel-head">
              <div>
                <h2 id="recovery-heading">恢复</h2>
                <p>失败步骤可在此重试，其余步骤等待对账。</p>
              </div>
            </div>
            {failedSteps.length === 0 ? (
              <div className="empty-inline">无失败步骤</div>
            ) : (
              <ul className="recovery-list">
                {failedSteps.map((step) => (
                  <li key={step.id}>
                    失败步骤 {step.ordinal}：{step.title}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="panel execution-panel" aria-labelledby="timeline-heading">
            <div className="panel-head">
              <div>
                <h2 id="timeline-heading">执行时间线</h2>
                <p>事件流按 seq 增量合并。</p>
              </div>
              <span className="stream-state" data-testid="stream-state">
                <StateDot tone={connected ? 'running' : 'muted'} />
                {connected ? '实时更新中' : '连接中断，正在重连'}
              </span>
            </div>
            {events.length === 0 ? (
              <div className="empty-inline">暂无事件</div>
            ) : (
              <ul className="timeline-list" aria-label="执行时间线">
                {events.map((event) => {
                  const payloadText = eventPayloadText(event.payload)
                  const prUrl = eventPrUrl(event.payload)
                  return (
                    <li key={event.eventId} className="timeline-item" data-testid={`event-${event.seq}`}>
                      <span className="seq">#{event.seq}</span>
                      <span className="kind">{event.kind}</span>
                      {payloadText ? <code className="timeline-payload">{payloadText}</code> : null}
                      {prUrl ? (
                        <a
                          className="timeline-pr"
                          href={prUrl}
                          target="_blank"
                          rel="noreferrer noopener"
                        >
                          查看 PR
                        </a>
                      ) : null}
                    </li>
                  )
                })}
              </ul>
            )}
          </section>
        </div>
      </div>
    </AppShell>
  )
}
