import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { EventPage, IssueDetail, PlanDetail, PlanStep } from '../api/issues'
import { RouterProvider } from '../router'
import IssueCockpit, { canRetry, type CockpitApi } from './IssueCockpit'

function makeDetail(status: IssueDetail['task']['status'] = 'executing'): IssueDetail {
  return {
    task: {
      id: 'task-1',
      workspaceId: 'ws-1',
      name: '修复登录',
      mode: 'ordinary',
      status,
      version: 3,
      planBranch: 'aladdin/12345678',
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    },
    plan: { id: 'plan-1', revision: 2, status: 'executing', contentHash: 'h', stepCount: 2 },
    acceptance: null,
  }
}

const steps: PlanStep[] = [
  { id: 'step-1', ordinal: 1, title: '改代码', body: '', agentRef: 'a1', status: 'completed', version: 1 },
  { id: 'step-2', ordinal: 2, title: '跑测试', body: '', agentRef: 'a2', status: 'failed', version: 4 },
]

function makePlan(): PlanDetail {
  return {
    plan: {
      id: 'plan-1',
      revision: 2,
      status: 'executing',
      contentHash: 'h',
      contentMd: '# plan',
      version: 2,
    },
    steps,
  }
}

function makeApi(overrides: Partial<CockpitApi> = {}): CockpitApi {
  return {
    fetchIssue: vi.fn().mockResolvedValue(makeDetail()),
    fetchPlan: vi.fn().mockResolvedValue(makePlan()),
    retryStep: vi.fn().mockResolvedValue({ attemptNo: 2 }),
    ...overrides,
  }
}

/** 用可控 fetcher 替代真实轮询，避免测试依赖定时器与网络。 */
function streamOf(pages: EventPage[]) {
  let index = 0
  return vi.fn().mockImplementation(async () => {
    const page = pages[Math.min(index, pages.length - 1)]
    index += 1
    return page
  })
}

/** AppShell 依赖路由上下文，测试统一包一层 RouterProvider。 */
function renderCockpit(props: Parameters<typeof IssueCockpit>[0]) {
  return render(
    <RouterProvider>
      <IssueCockpit {...props} />
    </RouterProvider>,
  )
}

describe('canRetry', () => {
  it('只有 failed 步骤显示重试入口', () => {
    expect(canRetry(steps[1])).toBe(true)
    expect(canRetry(steps[0])).toBe(false)
    expect(canRetry({ ...steps[0], status: 'reconciling' })).toBe(false)
  })
})

describe('IssueCockpit', () => {
  it('渲染任务状态、计划步骤与失败恢复入口', async () => {
    const api = makeApi()
    renderCockpit({ taskId: 'task-1', api })

    expect(await screen.findByText('修复登录')).toBeInTheDocument()
    expect(screen.getByTestId('task-status')).toHaveTextContent('执行中')
    expect(screen.getByTestId('step-status-1')).toHaveTextContent('已完成')
    expect(screen.getByTestId('step-status-2')).toHaveTextContent('失败')
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    expect(screen.getByText(/失败步骤 2/)).toBeInTheDocument()
    expect(api.fetchPlan).toHaveBeenCalledWith('task-1', 2)
  })

  it('执行时间线按 seq 增量合并，重复 eventId 不重复渲染', async () => {
    const page1: EventPage = {
      events: [
        { seq: 1, eventId: 'e1', kind: 'task.created', payload: {}, createdAt: '' },
        { seq: 2, eventId: 'e2', kind: 'step.dispatched', payload: {}, createdAt: '' },
      ],
      nextSeq: 2,
    }
    // 重连后服务端重复返回 e2，并携带新的 e4（seq 允许有洞）
    const page2: EventPage = {
      events: [
        { seq: 2, eventId: 'e2', kind: 'step.dispatched', payload: {}, createdAt: '' },
        { seq: 4, eventId: 'e4', kind: 'step.done', payload: {}, createdAt: '' },
      ],
      nextSeq: 4,
    }

    const api = makeApi()
    renderCockpit({
      taskId: 'task-1',
      api,
      eventsFetcher: streamOf([page1, page2]),
    })

    await screen.findByTestId('event-1')
    await screen.findByTestId('event-4')
    expect(screen.getByTestId('event-2')).toBeInTheDocument()
    expect(screen.queryAllByText(/step\.dispatched/)).toHaveLength(1)
  })

  it('点击重试调用接口并刷新状态', async () => {
    const api = makeApi()
    const user = userEvent.setup()
    renderCockpit({ taskId: 'task-1', api })

    const button = await screen.findByRole('button', { name: '重试' })
    await user.click(button)

    await waitFor(() => {
      expect(api.retryStep).toHaveBeenCalledWith('step-2', '', 4)
    })
    expect(api.fetchIssue).toHaveBeenCalledTimes(2)
  })
})
