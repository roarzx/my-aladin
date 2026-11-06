import { request } from './issues'

export interface Workspace {
  id: string
  name: string
  role: string
}

export interface Machine {
  id: string
  workspaceId: string
  name: string
  kind: string
  status: string
  capabilities: unknown[]
  cliVersion: string
  lastSeenAt: string | null
}

export interface Agent {
  id: string
  workspaceId: string
  name: string
  ownerId: number
  visibility: string
  runtimeType: string
  machineId: string
  workDir: string
  model: string
  thinkingLevel?: string
  systemPrompt: string
  capabilitySet: unknown
  envVars: unknown
  enabled: boolean
}

export interface Repository {
  id: string
  workspaceId: string
  url: string
  defaultBranch: string
  provider: string
}

export function fetchWorkspaces(): Promise<Workspace[]> {
  return request<Workspace[]>('/workspaces')
}

export function fetchMachines(workspaceId: string): Promise<Machine[]> {
  return request<Machine[]>(`/machines?workspaceId=${encodeURIComponent(workspaceId)}`)
}

export function createMachine(body: {
  workspaceId: string
  name: string
  kind: string
}): Promise<{ machine: Machine; token: string; expiresAt: string }> {
  return request<{ machine: Machine; token: string; expiresAt: string }>('/machines', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function issueMachineToken(
  machineId: string,
): Promise<{ token: string; expiresAt: string }> {
  return request<{ token: string; expiresAt: string }>(
    `/machines/${encodeURIComponent(machineId)}/token`,
    { method: 'POST' },
  )
}

export function fetchAgents(workspaceId: string): Promise<Agent[]> {
  return request<Agent[]>(`/agents?workspaceId=${encodeURIComponent(workspaceId)}`)
}

export interface AgentBody {
  workspaceId: string
  name: string
  machineId: string
  workDir: string
  systemPrompt?: string
  visibility?: string
  model?: string
  enabled?: boolean
}

export function createAgent(body: AgentBody): Promise<Agent> {
  return request<Agent>('/agents', { method: 'POST', body: JSON.stringify(body) })
}

export function updateAgent(id: string, patch: Partial<Omit<AgentBody, 'workspaceId'>>): Promise<Agent> {
  return request<Agent>(`/agents/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  })
}

export function disableAgent(id: string): Promise<Agent> {
  return request<Agent>(`/agents/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function fetchRepositories(workspaceId: string): Promise<Repository[]> {
  return request<Repository[]>(`/repositories?workspaceId=${encodeURIComponent(workspaceId)}`)
}

export function createRepository(body: {
  workspaceId: string
  url: string
  defaultBranch?: string
  provider?: string
}): Promise<Repository> {
  return request<Repository>('/repositories', { method: 'POST', body: JSON.stringify(body) })
}

export function createIssue(
  workspaceId: string,
  body: {
    name: string
    agentIds: string[]
    plannerAgentId: string
    repositories: string[]
  },
): Promise<{ id: string; status: string }> {
  return request<{ id: string; status: string }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/issues`,
    { method: 'POST', body: JSON.stringify(body) },
  )
}
