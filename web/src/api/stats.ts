import { request } from './issues'

/** 监控聚合（GET /stats/overview），口径见后端 internal/stats。 */

export interface DayCount {
  date: string
  count: number
}

export interface DayTokens {
  date: string
  input: number
  output: number
}

export interface NamedCount {
  name: string
  count: number
}

export interface ToolStat {
  name: string
  count: number
  failed: number
  p50Ms: number
  p95Ms: number
}

export interface ModelTokens {
  model: string
  input: number
  output: number
}

export interface AgentStats {
  agentId: string
  name: string
  runtimeType: string
  model: string
  runs: number
  finished: number
  succeeded: number
  failed: number
  successRate: number
  p50Ms: number
  p95Ms: number
  inputTokens: number
  outputTokens: number
  toolCalls: number
  lastRunAt: string
}

export interface StatsOverview {
  days: number
  generatedAt: string
  issues: {
    total: number
    byStatus: Record<string, number>
    created: DayCount[]
    completed: DayCount[]
  }
  runs: {
    total: number
    byStatus: Record<string, number>
    finished: number
    succeeded: number
    failed: number
    cancelled: number
    successRate: number
    errorRate: number
    p50Ms: number
    p95Ms: number
    retried: number
  }
  failures: NamedCount[]
  tools: ToolStat[]
  toolsByDay: DayCount[]
  tokens: {
    input: number
    output: number
    byModel: ModelTokens[]
    daily: DayTokens[]
  }
  machines: NamedCount[]
  availability: number
  agents: AgentStats[]
}

export function fetchStats(days = 7): Promise<StatsOverview> {
  return request<StatsOverview>(`/stats/overview?days=${days}`)
}
