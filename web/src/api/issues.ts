export type TaskStatus =
  | 'pending'
  | 'plan_confirming'
  | 'executing'
  | 'paused'
  | 'cancelling'
  | 'chatting'
  | 'cancelled'
  | 'failed'

export interface Task {
  id: string
  workspaceId: string
  name: string
  mode: string
  status: TaskStatus
  version: number
  planBranch?: string
  createdAt: string
  updatedAt: string
  cancelRequestedAt?: string
}

export interface PlanSummary {
  id: string
  revision: number
  status: string
  contentHash: string
  stepCount: number
}

export interface IssueAgent {
  ref: string
  agentId: string
  name: string
  role: string
}

export interface IssueRepository {
  id: string
  url: string
  defaultBranch: string
}

export interface IssueDetail {
  task: Task
  plan: PlanSummary | null
  acceptance: unknown
  agents?: IssueAgent[]
  repositories?: IssueRepository[]
}

export interface PlanStep {
  id: string
  ordinal: number
  title: string
  body: string
  agentRef: string
  status: string
  version: number
}

export interface PlanDetail {
  plan: {
    id: string
    revision: number
    status: string
    contentHash: string
    contentMd: string
    version: number
  } | null
  steps: PlanStep[]
}

export interface StreamEvent {
  seq: number
  eventId: string
  kind: string
  payload: unknown
  createdAt: string
}

export interface EventPage {
  events: StreamEvent[]
  nextSeq: number
}

export class ApiError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

const API_BASE: string = import.meta.env.VITE_API_BASE ?? ''
// 本地开发：`web/.env.development` 提供 VITE_DEV_TOKEN，免去手动设置 localStorage。
const DEV_TOKEN: string = import.meta.env.VITE_DEV_TOKEN ?? ''

export function currentToken(): string {
  return localStorage.getItem('aladdin_token') || DEV_TOKEN
}

function authHeaders(): Record<string, string> {
  const token = currentToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...(init?.headers ?? {}),
    },
  })
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    const err = body?.error
    throw new ApiError(err?.code ?? `HTTP_${res.status}`, err?.message ?? res.statusText)
  }
  return body.data as T
}

export function fetchIssues(limit = 50): Promise<Task[]> {
  return request<Task[]>(`/issues?limit=${limit}`)
}

export function fetchIssue(taskId: string): Promise<IssueDetail> {
  return request<IssueDetail>(`/issues/${encodeURIComponent(taskId)}`)
}

export function fetchPlan(taskId: string, revision: number): Promise<PlanDetail> {
  return request<PlanDetail>(
    `/issues/${encodeURIComponent(taskId)}/plans/${revision}?allowMissing=true`,
  )
}

/** 事件流端点自带 { data, nextSeq } 包装，不走 request() 的 data 解包。 */
export async function fetchEvents(taskId: string, after: number): Promise<EventPage> {
  const res = await fetch(
    `${API_BASE}/issues/${encodeURIComponent(taskId)}/events?after=${after}`,
    { headers: authHeaders() },
  )
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    const err = body?.error
    throw new ApiError(err?.code ?? `HTTP_${res.status}`, err?.message ?? res.statusText)
  }
  return { events: (body.data ?? []) as StreamEvent[], nextSeq: body.nextSeq ?? after }
}

export function retryStep(
  stepId: string,
  instruction: string,
  expectedVersion: number,
): Promise<{ attemptNo: number }> {
  return request<{ attemptNo: number }>(`/steps/${encodeURIComponent(stepId)}/retry`, {
    method: 'POST',
    body: JSON.stringify({ instruction, expectedVersion }),
  })
}

export interface PlanStepInput {
  ordinal: number
  title: string
  body: string
  agentRef: string
}

export interface PlanProposal {
  contentMd: string
  steps: PlanStepInput[]
}

export function submitPlan(
  taskId: string,
  proposal: PlanProposal,
): Promise<{ planId: string; revision: number; steps: number }> {
  return request<{ planId: string; revision: number; steps: number }>(
    `/issues/${encodeURIComponent(taskId)}/plans`,
    { method: 'POST', body: JSON.stringify(proposal) },
  )
}

export function confirmPlan(
  planId: string,
  revision: number,
  contentHash: string,
): Promise<{ revision: number; confirmedAt: string }> {
  return request<{ revision: number; confirmedAt: string }>(
    `/plans/${encodeURIComponent(planId)}/confirm`,
    { method: 'POST', body: JSON.stringify({ revision, contentHash }) },
  )
}

export function startTask(
  taskId: string,
  expectedVersion: number,
): Promise<{ startedAt: string; status: TaskStatus }> {
  return request<{ startedAt: string; status: TaskStatus }>(
    `/issues/${encodeURIComponent(taskId)}/start`,
    { method: 'POST', body: JSON.stringify({ expectedVersion }) },
  )
}
