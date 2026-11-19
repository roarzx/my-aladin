import { useCallback, useEffect, useMemo, useState, type FormEvent, type KeyboardEvent, type ReactNode } from 'react'

import {
  cancelIssue,
  confirmPlan,
  downloadArtifact,
  fetchArtifacts,
  fetchEvents,
  fetchIssue,
  fetchPlan,
  reconcileStep,
  retryStep,
  startTask,
  submitPlan,
  type Artifact,
  type EventPage,
  type IssueDetail,
  type PlanDetail,
  type PlanStep,
  type TaskStatus,
} from '../api/issues'
import { clearTaskDraft, readTaskDraft } from '../api/taskDraft'
import AppShell from '../components/AppShell'
import AuditTimeline from '../components/AuditTimeline'
import { MarkdownView } from '../components/markdown'
import { EmptyState, StateDot, StateTag, toast, type TagTone } from '../components/ui'
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
  /** 人工对账裁决（POST /steps/{id}/reconcile）。 */
  reconcileStep?: typeof reconcileStep
  /** 取消任务（POST /issues/{id}/cancel）。 */
  cancelIssue?: typeof cancelIssue
  /** 任务产物列表（GET /issues/{id}/artifacts）。 */
  fetchArtifacts?: typeof fetchArtifacts
  /** 产物下载。 */
  downloadArtifact?: typeof downloadArtifact
}

const defaultApi: CockpitApi = {
  fetchIssue,
  fetchPlan,
  retryStep,
  submitPlan,
  confirmPlan,
  startTask,
  reconcileStep,
  cancelIssue,
  fetchArtifacts,
  downloadArtifact,
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
  completed: '已完成',
  cancelled: '已取消',
  failed: '已失败',
}

const STATUS_TONES: Record<TaskStatus, TagTone> = {
  pending: 'pending',
  plan_confirming: 'review',
  executing: 'running',
  paused: 'paused',
  cancelling: 'queued',
  chatting: 'review',
  completed: 'done',
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

/** 计划状态中文映射与色调。 */
const PLAN_STATUS_META: Record<string, { label: string; tone: TagTone }> = {
  idle: { label: '待确认', tone: 'review' },
  draft: { label: '草稿', tone: 'draft' },
  submitted: { label: '待确认', tone: 'review' },
  confirmed: { label: '已确认', tone: 'queued' },
  executing: { label: '执行中', tone: 'running' },
  paused: { label: '已暂停', tone: 'paused' },
  stopped: { label: '已停止', tone: 'cancelled' },
  completed: { label: '已完成', tone: 'done' },
  superseded: { label: '已被取代', tone: 'muted' },
  cancelled: { label: '已取消', tone: 'cancelled' },
}

function planStatusMeta(status: string): { label: string; tone: TagTone } {
  return PLAN_STATUS_META[status] ?? { label: status, tone: 'muted' }
}

function formatBytes(size: number): string {
  if (!Number.isFinite(size) || size < 0) return '—'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

/** 产物卡片上的扩展名徽标。 */
function fileBadge(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) return 'FILE'
  return name.slice(dot + 1).toUpperCase().slice(0, 4)
}

function formatTime(value: string): string {
  const date = new Date(value || 0)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

interface StepDraft {
  title: string
  body: string
  agentRef: string
}

function emptyStepDraft(): StepDraft {
  return { title: '', body: '', agentRef: 'a1' }
}

/** 步骤卡片的状态色，仅用于呈现。 */
function stepCardClass(status: string): string {
  if (status === 'completed') return 'step-card done'
  if (status === 'failed') return 'step-card failed'
  if (status === 'running' || status === 'dispatched' || status === 'reconciling') {
    return 'step-card active'
  }
  return 'step-card'
}

type CockpitTab = 'chat' | 'records'

function Avatar({ kind, label }: { kind: 'user' | 'planner' | 'system'; label: string }) {
  return <div className={`msg-avatar ${kind}`}>{label}</div>
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
  const [reconciling, setReconciling] = useState<string | null>(null)
  const [tab, setTab] = useState<CockpitTab>('chat')
  const [infoOpen, setInfoOpen] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [draft, setDraft] = useState('')
  const [artifacts, setArtifacts] = useState<Artifact[]>([])

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
      if (api.fetchArtifacts) {
        try {
          setArtifacts(await api.fetchArtifacts(taskId))
        } catch {
          setArtifacts([])
        }
      }
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

  const onReconcile = useCallback(
    async (step: PlanStep, verdict: 'completed' | 'failed') => {
      const run = api.reconcileStep
      if (!run) {
        toast('当前环境不支持人工对账')
        return
      }
      setReconciling(step.id)
      setActionError(null)
      try {
        await run(step.id, verdict, '')
        await reload()
        toast(verdict === 'completed' ? '已按完成收敛' : '已按失败收敛')
      } catch (err) {
        setActionError(err instanceof Error ? err.message : String(err))
      } finally {
        setReconciling(null)
      }
    },
    [api, reload],
  )

  const failedSteps = useMemo(
    () => (planDetail?.steps ?? []).filter((s) => s.status === 'failed'),
    [planDetail],
  )

  const share = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(window.location.href)
      toast('已复制任务链接')
    } catch {
      toast('复制失败，请手动复制地址栏链接')
    }
  }, [])

  const onDownload = useCallback(
    async (item: Artifact) => {
      const run = api.downloadArtifact ?? downloadArtifact
      try {
        await run(taskId, item)
      } catch (err) {
        toast(`下载失败：${err instanceof Error ? err.message : String(err)}`)
      }
    },
    [api, taskId],
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
          description="请从 Issue 列表选择一个任务，再进入驾驶舱查看计划与执行记录。"
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
  const planMeta = activePlan ? planStatusMeta(activePlan.status) : null
  const planSubmittedAt = events.find((event) => event.kind === 'plan.submitted')?.createdAt ?? ''
  const planSubmittedLabel = planSubmittedAt ? formatTime(planSubmittedAt) : formatTime(task.updatedAt)

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

  const cancelTask = async () => {
    const run = api.cancelIssue
    if (!run) {
      toast('当前环境不支持取消任务')
      return
    }
    setCancelling(true)
    setActionError(null)
    try {
      await run(taskId, task.version)
      await reload()
      setInfoOpen(false)
      toast('已请求取消任务')
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setCancelling(false)
    }
  }

  const sendMessage = () => {
    if (!draft.trim()) return
    toast('本地后端尚未提供会话消息接口，接入后可在此继续对话')
  }

  const onComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      sendMessage()
    }
  }

  const titleNode: ReactNode = (
    <span className="cockpit-title">
      {task.name}
      <StateTag tone={statusTone}>{statusLabel}</StateTag>
    </span>
  )

  const stepActions = (step: PlanStep): ReactNode => {
    if (canRetry(step)) {
      return (
        <button
          type="button"
          className="button ghost small"
          disabled={retrying === step.id}
          onClick={() => void onRetry(step)}
        >
          {retrying === step.id ? '重试中…' : '重试'}
        </button>
      )
    }
    if (step.status === 'reconciling') {
      return (
        <div className="button-row">
          <button
            type="button"
            className="button ghost small"
            disabled={reconciling === step.id}
            onClick={() => void onReconcile(step, 'completed')}
          >
            对账为完成
          </button>
          <button
            type="button"
            className="button ghost small"
            disabled={reconciling === step.id}
            onClick={() => void onReconcile(step, 'failed')}
          >
            对账为失败
          </button>
        </div>
      )
    }
    return null
  }

  return (
    <AppShell
      title={titleNode}
      breadcrumb="我的 Issue / Issue 驾驶舱"
      className="cockpit-page"
      actions={
        <>
          <button type="button" className="button ghost" onClick={() => setInfoOpen(true)}>
            任务信息
          </button>
          <button type="button" className="button ghost" onClick={() => void share()}>
            分享
          </button>
          <button type="button" className="button ghost" onClick={() => void reload()}>
            刷新
          </button>
        </>
      }
    >
      <div className="cockpit-shell">
        <div className="cockpit-submeta">
          <span>
            状态：<strong data-testid="task-status">{statusLabel}</strong>
          </span>
          {task.planBranch ? <span>分支 {task.planBranch}</span> : null}
          <span>工作区 {task.workspaceId}</span>
          <span className="stream-state" data-testid="stream-state">
            <StateDot tone={connected ? 'running' : 'muted'} />
            {connected ? '实时更新中' : '连接中断，正在重连'}
          </span>
        </div>

        <div className="cockpit-tabs" role="tablist" aria-label="驾驶舱视图">
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'chat'}
            className={tab === 'chat' ? 'cockpit-tab active' : 'cockpit-tab'}
            onClick={() => setTab('chat')}
          >
            会话
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'records'}
            className={tab === 'records' ? 'cockpit-tab active' : 'cockpit-tab'}
            onClick={() => setTab('records')}
          >
            记录
          </button>
        </div>

        {streamError ? (
          <p role="alert" className="notice danger">
            事件流错误：{streamError.message}
          </p>
        ) : null}

        {tab === 'chat' ? (
          <div className="thread" aria-label="会话">
            <article className="msg">
              <Avatar kind="user" label="我" />
              <div className="msg-main">
                <div className="msg-head">
                  <strong>我</strong>
                  <span className="msg-time">{formatTime(task.createdAt)}</span>
                </div>
                <div className="msg-body">
                  <p className="msg-requirement">{task.name}</p>
                </div>
              </div>
            </article>

            {!activePlan ? (
              <article className="msg">
                <Avatar kind="planner" label="规" />
                <div className="msg-main">
                  <div className="msg-head">
                    <strong>规划</strong>
                    <span className="role-badge">待提交</span>
                  </div>
                  <div className="msg-status">
                    <StateDot tone="pending" />
                    尚未提交计划，补充计划后即可确认并启动执行。
                  </div>
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
                </div>
              </article>
            ) : (
              <article className="msg">
                <Avatar kind="planner" label="规" />
                <div className="msg-main">
                  <div className="msg-head">
                    <strong>规划</strong>
                    <span className="role-badge">执行</span>
                    <span className="msg-time">{planSubmittedLabel}</span>
                  </div>
                  <div className="msg-status">
                    <StateDot tone={planMeta ? planMeta.tone : 'muted'} />
                    {planMeta ? planMeta.label : activePlan.status} · 版本 r{activePlan.revision} ·{' '}
                    {detail.plan?.stepCount ?? steps.length} 步
                  </div>

                  {planDetail?.plan?.contentMd ? (
                    <div className="msg-body">
                      <MarkdownView text={planDetail.plan.contentMd} />
                    </div>
                  ) : null}

                  <ol className="step-list chat-steps">
                    {steps.map((step) => {
                      const meta = stepStatusMeta(step.status)
                      return (
                        <li
                          key={step.id}
                          className={stepCardClass(step.status)}
                          data-testid={`step-${step.ordinal}`}
                        >
                          <div className="step-top">
                            <span className="step-title">
                              {step.ordinal}. {step.title}
                            </span>
                            <span className="step-status" data-testid={`step-status-${step.ordinal}`}>
                              <StateTag tone={meta.tone}>{meta.label}</StateTag>
                            </span>
                          </div>
                          <div className="step-meta">
                            <span>@{step.agentRef}</span>
                            {stepActions(step)}
                          </div>
                        </li>
                      )
                    })}
                  </ol>

                  {planIdle ? (
                    <div className="msg-actions">
                      <button
                        type="button"
                        className="button ghost"
                        disabled={planBusy !== null || !api.confirmPlan}
                        onClick={() => void confirmOnly()}
                      >
                        {planBusy === 'confirm' ? '确认中…' : '确认计划'}
                      </button>
                      <button
                        type="button"
                        className="button primary"
                        disabled={planBusy !== null || !api.confirmPlan || !api.startTask}
                        onClick={() => void confirmAndStart()}
                      >
                        {planBusy === 'start' ? '启动中…' : '确认并启动'}
                      </button>
                    </div>
                  ) : null}

                  {failedSteps.length > 0 ? (
                    <div className="msg-status failed">
                      存在 {failedSteps.length} 个失败步骤，可在上方直接重试。
                    </div>
                  ) : null}
                </div>
              </article>
            )}

            {artifacts.length > 0 ? (
              <article className="msg" aria-label="任务产物">
                <Avatar kind="planner" label="产" />
                <div className="msg-main">
                  <div className="msg-head">
                    <strong>产物</strong>
                    <span className="msg-time">{artifacts.length} 个文件</span>
                  </div>
                  <div className="artifact-list">
                    {artifacts.map((item) => {
                      const name = item.path.split('/').pop() || item.path
                      return (
                        <button
                          type="button"
                          className="artifact-card"
                          key={item.id}
                          onClick={() => void onDownload(item)}
                          title={item.path}
                        >
                          <span className="artifact-icon" aria-hidden>
                            {fileBadge(name)}
                          </span>
                          <span className="artifact-meta">
                            <strong>{name}</strong>
                            <small>
                              {formatBytes(item.sizeBytes)}
                              {item.path !== name ? ` · ${item.path}` : ''}
                            </small>
                          </span>
                          <span className="artifact-action">下载</span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              </article>
            ) : null}

            {actionError ? (
              <p role="alert" className="error">
                操作失败：{actionError}
              </p>
            ) : null}
          </div>
        ) : (
          <div className="thread records" aria-label="执行记录">
            <AuditTimeline events={events} steps={steps} connected={connected} />
          </div>
        )}

        <form
          className="composer"
          onSubmit={(event) => {
            event.preventDefault()
            sendMessage()
          }}
        >
          <textarea
            className="composer-input"
            rows={2}
            value={draft}
            placeholder="输入 @ 选择 Agent、插件或流程…"
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onComposerKeyDown}
            aria-label="会话输入"
          />
          <div className="composer-bar">
            <button type="button" className="icon-btn" aria-label="添加附件" onClick={() => toast('本地后端尚未提供附件接口')}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
                <path d="M21 11.5 12.5 20a5 5 0 0 1-7-7l9-9a3.5 3.5 0 1 1 5 5l-9 9a2 2 0 0 1-3-3l8.5-8.5" />
              </svg>
            </button>
            <span className="composer-hint">按 Enter 发送，Shift + Enter 换行</span>
            <button type="button" className="icon-btn" aria-label="历史消息" onClick={() => setTab('records')}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
                <circle cx="12" cy="12" r="8.5" />
                <path d="M12 7.5V12l3 2" />
              </svg>
            </button>
            <button type="submit" className="send-btn" aria-label="发送" disabled={!draft.trim()}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <path d="M21 3 10.5 13.5" />
                <path d="M21 3 14 21l-3.5-7.5L3 10z" />
              </svg>
            </button>
          </div>
        </form>
      </div>

      {infoOpen ? (
        <div className="modal-backdrop" onClick={() => setInfoOpen(false)}>
          <div
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label="任务信息"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-head">
              <h3>任务信息</h3>
              <button type="button" className="button ghost small" onClick={() => setInfoOpen(false)}>
                关闭
              </button>
            </div>
            <div className="modal-body">
              <dl className="detail-grid">
                <div className="detail-item">
                  <dt>任务 ID</dt>
                  <dd className="mono">{task.id}</dd>
                </div>
                <div className="detail-item">
                  <dt>状态</dt>
                  <dd>{statusLabel}</dd>
                </div>
                <div className="detail-item">
                  <dt>模式</dt>
                  <dd>{task.mode}</dd>
                </div>
                <div className="detail-item">
                  <dt>工作区</dt>
                  <dd className="mono">{task.workspaceId}</dd>
                </div>
                <div className="detail-item">
                  <dt>计划分支</dt>
                  <dd className="mono">{task.planBranch || '—'}</dd>
                </div>
                <div className="detail-item">
                  <dt>计划版本</dt>
                  <dd>{activePlan ? `r${activePlan.revision} · ${planMeta?.label ?? activePlan.status}` : '未提交'}</dd>
                </div>
                <div className="detail-item">
                  <dt>创建时间</dt>
                  <dd>{formatTime(task.createdAt)}</dd>
                </div>
                <div className="detail-item">
                  <dt>更新时间</dt>
                  <dd>{formatTime(task.updatedAt)}</dd>
                </div>
              </dl>
              {actionError ? (
                <p role="alert" className="error">
                  操作失败：{actionError}
                </p>
              ) : null}
            </div>
            <div className="modal-foot">
              <button type="button" className="button ghost" onClick={() => setInfoOpen(false)}>
                关闭
              </button>
              <button
                type="button"
                className="button danger"
                disabled={cancelling || task.status === 'cancelled'}
                onClick={() => void cancelTask()}
              >
                {cancelling ? '取消中…' : '取消任务'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </AppShell>
  )
}
