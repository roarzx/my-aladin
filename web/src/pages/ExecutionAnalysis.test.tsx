import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RouterProvider } from '../router'
import ExecutionAnalysis from './ExecutionAnalysis'

const days = ['2026-01-01', '2026-01-02', '2026-01-03']

const overview = {
  days: 7,
  generatedAt: '2026-01-03T00:00:00Z',
  issues: {
    total: 4,
    byStatus: { executing: 1, completed: 3 },
    created: days.map((date) => ({ date, count: 1 })),
    completed: days.map((date) => ({ date, count: 1 })),
  },
  runs: {
    total: 6,
    byStatus: { completed: 4, failed: 2 },
    finished: 6,
    succeeded: 4,
    failed: 2,
    cancelled: 0,
    successRate: 4 / 6,
    errorRate: 2 / 6,
    p50Ms: 12000,
    p95Ms: 138000,
    retried: 2,
  },
  failures: [{ name: 'network', count: 2 }],
  tools: [{ name: 'ls', count: 9, failed: 1, p50Ms: 120, p95Ms: 400 }],
  toolsByDay: days.map((date) => ({ date, count: 3 })),
  tokens: {
    input: 28000,
    output: 500,
    byModel: [{ model: 'claude-sonnet-5-5', input: 28000, output: 500 }],
    daily: days.map((date) => ({ date, input: 10000, output: 200 })),
  },
  machines: [{ name: 'online', count: 1 }],
  availability: 1,
  agents: [
    {
      agentId: 'agent-123456',
      name: 'Claude',
      runtimeType: 'claude_code',
      model: 'claude-sonnet-5-5',
      runs: 6,
      finished: 6,
      succeeded: 4,
      failed: 2,
      successRate: 4 / 6,
      p50Ms: 12000,
      p95Ms: 138000,
      inputTokens: 28000,
      outputTokens: 500,
      toolCalls: 9,
      lastRunAt: '2026-01-03T00:00:00Z',
    },
  ],
}

function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: overview }) }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ExecutionAnalysis', () => {
  it('按 Agent 展示轮次、成功率与 token', async () => {
    stubFetch()
    render(
      <RouterProvider>
        <ExecutionAnalysis />
      </RouterProvider>,
    )

    expect(await screen.findByText('Claude')).toBeInTheDocument()
    expect(screen.getByText('Claude Code')).toBeInTheDocument()
    expect(screen.getByText('66.7%')).toBeInTheDocument()
    expect(screen.getByTestId('agent-table')).toBeInTheDocument()
    expect(screen.getByText('4 完成 / 2 失败')).toBeInTheDocument()
  })

  it('展示工具耗时、失败分类与 token 归属', async () => {
    stubFetch()
    render(
      <RouterProvider>
        <ExecutionAnalysis />
      </RouterProvider>,
    )

    expect(await screen.findByTestId('tool-table')).toBeInTheDocument()
    expect(screen.getByText('ls')).toBeInTheDocument()
    expect(screen.getByText('网络 / 超时')).toBeInTheDocument()
    expect(screen.getAllByText('claude-sonnet-5-5').length).toBeGreaterThan(0)
    // 上下文压缩未接入，要明确说出来。
    expect(screen.getByText(/上下文压缩.*尚未采集/)).toBeInTheDocument()
  })
})
