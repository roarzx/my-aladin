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
  failed?: number
}

export interface ModelTokens {
  model: string
  input: number
  output: number
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
    successRate: number
    p50Ms: number
    p95Ms: number
  }
  failures: NamedCount[]
  tools: NamedCount[]
  toolsByDay: DayCount[]
  tokens: {
    input: number
    output: number
    byModel: ModelTokens[]
    daily: DayTokens[]
  }
  machines: NamedCount[]
}

export function fetchStats(days = 7): Promise<StatsOverview> {
  return request<StatsOverview>(`/stats/overview?days=${days}`)
}
