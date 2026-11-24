import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { StatsOverview } from '../api/stats'
import { RouterProvider } from '../router'
import Dashboard from './Dashboard'

const tasks = [
  {
    id: 'task-1',
    workspaceId: 'ws-1',
    name: '演示任务',
    mode: 'ordinary',
    status: 'executing',
    version: 3,
    agentName: 'Claude',
    requirement: '把登录页改好',
    latestRunStatus: 'running',
    latestRunAt: '2026-01-01T00:00:00Z',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  },
]

function makeStats(): StatsOverview {
  const days = ['2026-01-01', '2026-01-02', '2026-01-03']
  return {
    days: 7,
    generatedAt: '2026-01-03T00:00:00Z',
    issues: {
      total: 3,
      byStatus: { executing: 1, completed: 2 },
      created: days.map((date, index) => ({ date, count: index })),
      completed: days.map((date, index) => ({ date, count: index === 2 ? 2 : 0 })),
    },
    runs: {
      total: 4,
      byStatus: { completed: 3, failed: 1 },
      finished: 4,
      succeeded: 3,
      successRate: 0.75,
      p50Ms: 12000,
      p95Ms: 138000,
    },
    failures: [{ name: 'network', count: 1 }],
    tools: [{ name: 'ls', count: 9, failed: 1 }, { name: 'Edit', count: 4 }],
    toolsByDay: days.map((date) => ({ date, count: 3 })),
    tokens: {
      input: 28000,
      output: 500,
      byModel: [{ model: 'claude-sonnet-5-5', input: 28000, output: 500 }],
      daily: days.map((date) => ({ date, input: 10000, output: 200 })),
    },
    machines: [{ name: 'online', count: 1 }, { name: 'offline', count: 2 }],
  }
}

function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string) => {
      const data = String(url).includes('/stats/overview') ? makeStats() : tasks
      return { ok: true, json: async () => ({ data }) }
    }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Dashboard', () => {
  it('展示成功率、耗时与 token 汇总', async () => {
    stubFetch()
    render(
      <RouterProvider>
        <Dashboard />
      </RouterProvider>,
    )

    expect(await screen.findByText('上报完成率')).toBeInTheDocument()
    expect(screen.getByText('75%')).toBeInTheDocument()
    expect(screen.getByText('2m18s')).toBeInTheDocument()
    expect(screen.getByText('近 7 天 Token')).toBeInTheDocument()
    expect(screen.getByText('29k')).toBeInTheDocument()
    expect(screen.getByText('3/4 轮；只算终态')).toBeInTheDocument()
  })

  it('渲染每日趋势与失败分类', async () => {
    stubFetch()
    render(
      <RouterProvider>
        <Dashboard />
      </RouterProvider>,
    )

    expect(await screen.findByTestId('chart-issues')).toBeInTheDocument()
    expect(screen.getByTestId('chart-tokens')).toBeInTheDocument()
    // 失败分类要翻译成人话。
    expect(screen.getByText('网络 / 超时')).toBeInTheDocument()
    // 机器在线数。
    expect(screen.getByText('在线')).toBeInTheDocument()
    expect(screen.getByText('离线')).toBeInTheDocument()
  })
})
