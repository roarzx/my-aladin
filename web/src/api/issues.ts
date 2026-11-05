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

export interface IssueDetail {
  task: Task
  plan: PlanSummary | null
  acceptance: unknown
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

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('aladdin_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
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
