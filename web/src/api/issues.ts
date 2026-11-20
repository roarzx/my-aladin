export type TaskStatus = 'pending' | 'executing' | 'paused' | 'completed' | 'cancelled' | 'failed'

/** 一轮执行：一次把提示词交给 Agent，直到 turn_done / turn_fail。 */
export interface IssueRun {
  id: string
  taskId: string
  agentId: string
  sessionId?: string
  executionId: string
  status: 'dispatched' | 'running' | 'completed' | 'failed' | 'cancelled'
  reply?: string
  errorClass?: string
  detail?: string
  createdAt: string
  updatedAt: string
}

export interface Task {
  id: string
  workspaceId: string
  name: string
  mode: string
  status: TaskStatus
  version: number
  planBranch?: string
  /** 列表卡片用的摘要字段（详情接口不返回）。 */
  requirement?: string
  agentName?: string
  latestRunStatus?: IssueRun['status']
  latestRunAt?: string
  createdAt: string
  updatedAt: string
  cancelRequestedAt?: string
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
  latestRun?: IssueRun | null
  acceptance: unknown
  agents?: IssueAgent[]
  repositories?: IssueRepository[]
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

export interface Artifact {
  id: string
  taskId: string
  stepId?: string
  attemptId?: string
  path: string
  sha256: string
  sizeBytes: number
  contentType: string
}

export function fetchArtifacts(taskId: string): Promise<Artifact[]> {
  return request<Artifact[]>(`/issues/${encodeURIComponent(taskId)}/artifacts`)
}

/** 产物下载：带令牌请求后转成 blob 触发浏览器保存。 */
export async function downloadArtifact(taskId: string, artifact: Artifact): Promise<void> {
  const url = `${API_BASE}/issues/${encodeURIComponent(taskId)}/artifacts/${encodeURIComponent(artifact.id)}/content`
  const res = await fetch(url, { headers: authHeaders() })
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    throw new ApiError(body?.error?.code ?? `HTTP_${res.status}`, body?.error?.message ?? res.statusText)
  }
  const blob = await res.blob()
  const objectUrl = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectUrl
  link.download = artifact.path.split('/').pop() || 'artifact'
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(objectUrl)
}

/**
 * 会话追问：后端把消息交给 Agent，形成新一轮执行；
 * Agent 的回复随后写入事件流（message.assistant）。
 */
export function sendMessage(taskId: string, content: string): Promise<IssueRun> {
  return request<IssueRun>(`/issues/${encodeURIComponent(taskId)}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

export function fetchIssues(limit = 50): Promise<Task[]> {
  return request<Task[]>(`/issues?limit=${limit}`)
}

export function fetchIssue(taskId: string): Promise<IssueDetail> {
  return request<IssueDetail>(`/issues/${encodeURIComponent(taskId)}`)
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

export function fetchRuns(taskId: string): Promise<IssueRun[]> {
  return request<IssueRun[]>(`/issues/${encodeURIComponent(taskId)}/runs`)
}

/** 开始或继续一轮执行；提示词为空时后端用需求正文。 */
export function startIssue(taskId: string, prompt = ''): Promise<IssueRun> {
  return request<IssueRun>(`/issues/${encodeURIComponent(taskId)}/start`, {
    method: 'POST',
    body: JSON.stringify({ prompt }),
  })
}

export function cancelIssue(taskId: string, reason = ''): Promise<unknown> {
  return request<unknown>(`/issues/${encodeURIComponent(taskId)}/cancel`, {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}
