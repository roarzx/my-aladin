import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { EventPage, IssueDetail, IssueRun } from '../api/issues'
import { RouterProvider } from '../router'
import IssueCockpit, { type CockpitApi } from './IssueCockpit'

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
    latestRun: null,
    acceptance: null,
    agents: [{ ref: 'a1', agentId: 'agent-1', name: 'Claude', role: 'planner' }],
  }
}

function makeRun(overrides: Partial<IssueRun> = {}): IssueRun {
  return {
    id: 'run-1',
    taskId: 'task-1',
    agentId: 'agent-1',
    executionId: 'exec-1',
    status: 'dispatched',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeApi(overrides: Partial<CockpitApi> = {}): CockpitApi {
  return {
    fetchIssue: vi.fn().mockResolvedValue(makeDetail()),
    fetchArtifacts: vi.fn().mockResolvedValue([]),
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

describe('IssueCockpit', () => {
  it('渲染标题与任务状态', async () => {
    renderCockpit({ taskId: 'task-1', api: makeApi() })

    expect(await screen.findByRole('heading', { name: /修复登录/ })).toBeInTheDocument()
    expect(screen.getByTestId('task-status')).toHaveTextContent('执行中')
    expect(screen.getByRole('button', { name: '取消' })).toBeInTheDocument()
  })

  it('执行中禁用输入，结束后可继续发消息', async () => {
    const api = makeApi({ fetchIssue: vi.fn().mockResolvedValue(makeDetail('completed')) })
    renderCockpit({ taskId: 'task-1', api, eventsFetcher: streamOf([{ events: [], nextSeq: 0 }]) })

    const input = await screen.findByLabelText('会话输入')
    expect(input).not.toBeDisabled()
    expect(screen.getByRole('button', { name: '继续执行' })).toBeInTheDocument()
  })

  it('发送消息调用消息接口并刷新', async () => {
    const api = makeApi({
      fetchIssue: vi.fn().mockResolvedValue(makeDetail('completed')),
      sendMessage: vi.fn().mockResolvedValue(makeRun()),
    })
    const user = userEvent.setup()
    renderCockpit({ taskId: 'task-1', api, eventsFetcher: streamOf([{ events: [], nextSeq: 0 }]) })

    const input = await screen.findByLabelText('会话输入')
    await user.type(input, '写好了吗？')
    await user.click(screen.getByRole('button', { name: '发送' }))

    await waitFor(() => {
      expect(api.sendMessage).toHaveBeenCalledWith('task-1', '写好了吗？')
    })
    expect(api.fetchIssue).toHaveBeenCalledTimes(2)
  })

  it('点「开始执行」调用开始接口', async () => {
    const api = makeApi({
      fetchIssue: vi.fn().mockResolvedValue(makeDetail('pending')),
      startIssue: vi.fn().mockResolvedValue(makeRun()),
    })
    const user = userEvent.setup()
    renderCockpit({ taskId: 'task-1', api, eventsFetcher: streamOf([{ events: [], nextSeq: 0 }]) })

    await user.click(await screen.findByRole('button', { name: '开始执行' }))
    await waitFor(() => expect(api.startIssue).toHaveBeenCalledWith('task-1'))
  })

  it('展示会话中的人机消息与状态变更', async () => {
    const page: EventPage = {
      events: [
        { seq: 1, eventId: 'e1', kind: 'message.user', payload: { text: '写好了吗？' }, createdAt: '2026-01-01T00:01:00Z' },
        { seq: 2, eventId: 'e2', kind: 'message.assistant', payload: { text: '已经写好了。' }, createdAt: '2026-01-01T00:02:00Z' },
        {
          seq: 3,
          eventId: 'e3',
          kind: 'issue.status',
          payload: { from: 'executing', to: 'completed', reason: '验证通过' },
          createdAt: '2026-01-01T00:03:00Z',
        },
      ],
      nextSeq: 3,
    }
    renderCockpit({ taskId: 'task-1', api: makeApi(), eventsFetcher: streamOf([page]) })

    expect(await screen.findByText('写好了吗？')).toBeInTheDocument()
    expect(await screen.findByText('已经写好了。')).toBeInTheDocument()
    expect(await screen.findByTestId('thread-status')).toHaveTextContent('执行中 → 已完成')
  })

  it('记录页按 seq 增量合并，重复 eventId 不重复渲染', async () => {
    const page1: EventPage = {
      events: [
        { seq: 1, eventId: 'e1', kind: 'task.created', payload: { mode: 'ordinary' }, createdAt: '' },
        { seq: 2, eventId: 'e2', kind: 'turn.started', payload: {}, createdAt: '' },
      ],
      nextSeq: 2,
    }
    // 重连后服务端重复返回 e2，并携带新的 e4（seq 允许有洞）
    const page2: EventPage = {
      events: [
        { seq: 2, eventId: 'e2', kind: 'turn.started', payload: {}, createdAt: '' },
        { seq: 4, eventId: 'e4', kind: 'turn.done', payload: { duration_ms: 1200 }, createdAt: '' },
      ],
      nextSeq: 4,
    }

    const user = userEvent.setup()
    renderCockpit({
      taskId: 'task-1',
      api: makeApi(),
      eventsFetcher: streamOf([page1, page2]),
    })

    await user.click(await screen.findByRole('tab', { name: '记录' }))

    await screen.findByTestId('event-1')
    await screen.findByTestId('event-4')
    expect(screen.getByTestId('event-2')).toBeInTheDocument()
    expect(screen.queryAllByText('开始一轮执行')).toHaveLength(1)
  })

  it('展示产物并提供下载', async () => {
    const download = vi.fn().mockResolvedValue(undefined)
    const api = makeApi({
      fetchArtifacts: vi.fn().mockResolvedValue([
        { id: 'art-1', taskId: 'task-1', path: 'dist/index.html', sha256: 'h', sizeBytes: 2048, contentType: 'text/html' },
      ]),
      downloadArtifact: download as unknown as CockpitApi['downloadArtifact'],
    })
    const user = userEvent.setup()
    renderCockpit({ taskId: 'task-1', api, eventsFetcher: streamOf([{ events: [], nextSeq: 0 }]) })

    expect(await screen.findByText('index.html')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下载' }))
    await waitFor(() => expect(download).toHaveBeenCalledWith('task-1', expect.objectContaining({ id: 'art-1' })))
  })
})
