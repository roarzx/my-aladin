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

    expect(await screen.findByRole('heading', { name: /修复登录/ })).toBeInTheDocument()
    expect(screen.getByTestId('task-status')).toHaveTextContent('执行中')
    expect(screen.getByTestId('step-status-1')).toHaveTextContent('已完成')
    expect(screen.getByTestId('step-status-2')).toHaveTextContent('失败')
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
    expect(screen.getByText(/存在 1 个失败步骤/)).toBeInTheDocument()
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
    const user = userEvent.setup()
    renderCockpit({
      taskId: 'task-1',
      api,
      eventsFetcher: streamOf([page1, page2]),
    })

    // 时间线在「记录」标签页内
    await user.click(await screen.findByRole('tab', { name: '记录' }))

    await screen.findByTestId('event-1')
    await screen.findByTestId('event-4')
    expect(screen.getByTestId('event-2')).toBeInTheDocument()
    expect(screen.queryAllByText(/步骤已派发/)).toHaveLength(1)
  })

  it('会话发送追问调用消息接口并刷新', async () => {
    const api = makeApi({
      fetchIssue: vi.fn().mockResolvedValue(makeDetail('completed')),
      sendMessage: vi.fn().mockResolvedValue({ taskId: 'task-1', planId: 'p', revision: 3, status: 'executing' }),
    })
    const user = userEvent.setup()
    renderCockpit({
      taskId: 'task-1',
      api,
      eventsFetcher: streamOf([{ events: [], nextSeq: 0 }]),
    })

    const input = await screen.findByLabelText('会话输入')
    await user.type(input, '写好了吗？')
    await user.click(screen.getByRole('button', { name: '发送' }))

    await waitFor(() => {
      expect(api.sendMessage).toHaveBeenCalledWith('task-1', '写好了吗？')
    })
    expect(api.fetchIssue).toHaveBeenCalledTimes(2)
  })

  it('展示会话中的人机消息', async () => {
    const api = makeApi()
    const page: EventPage = {
      events: [
        { seq: 1, eventId: 'e1', kind: 'plan.submitted', payload: { revision: 2 }, createdAt: '2026-01-01T00:00:00Z' },
        { seq: 2, eventId: 'e2', kind: 'message.user', payload: { text: '写好了吗？' }, createdAt: '2026-01-01T00:01:00Z' },
        { seq: 3, eventId: 'e3', kind: 'message.assistant', payload: { text: '已经写好了。' }, createdAt: '2026-01-01T00:02:00Z' },
      ],
      nextSeq: 3,
    }
    renderCockpit({ taskId: 'task-1', api, eventsFetcher: streamOf([page]) })

    expect(await screen.findByText('写好了吗？')).toBeInTheDocument()
    expect(await screen.findByText('已经写好了。')).toBeInTheDocument()
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
