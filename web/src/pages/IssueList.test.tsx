import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RouterProvider } from '../router'
import IssueList from './IssueList'
import TokenGate from './TokenGate'

const tasks = [
  {
    id: 'task-1',
    workspaceId: 'ws-1',
    name: '演示任务',
    mode: 'ordinary',
    status: 'chatting',
    version: 3,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  },
]

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: status < 400,
      status,
      statusText: '',
      json: async () => body,
    }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('IssueList', () => {
  it('渲染任务列表并链接到驾驶舱', async () => {
    stubFetch({ data: tasks })
    render(
      <RouterProvider>
        <IssueList />
      </RouterProvider>,
    )

    const link = await screen.findByRole('link', { name: /演示任务/ })
    expect(link).toHaveAttribute('href', '/task/detail?id=task-1')
    expect(within(link).getByText('对话中')).toBeInTheDocument()
  })

  it('看板四列在空列表时展示占位', async () => {
    stubFetch({ data: [] })
    render(
      <RouterProvider>
        <IssueList />
      </RouterProvider>,
    )
    expect(await screen.findByRole('heading', { name: '待处理' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '执行中' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '异常处理' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '已完成' })).toBeInTheDocument()
    expect(screen.getAllByText('暂无任务')).toHaveLength(4)
  })

  it('接口失败显示错误信息', async () => {
    stubFetch({ error: { code: 'UNAUTHENTICATED', message: '缺少 Bearer 令牌' } }, 401)
    render(
      <RouterProvider>
        <IssueList />
      </RouterProvider>,
    )
    expect(await screen.findByRole('alert')).toHaveTextContent('缺少 Bearer 令牌')
  })
})

describe('TokenGate', () => {
  it('提交令牌后回调', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn()
    render(<TokenGate onSave={onSave} />)
    await user.type(screen.getByLabelText('访问令牌'), 'devtoken')
    await user.click(screen.getByRole('button', { name: '进入' }))
    expect(onSave).toHaveBeenCalledWith('devtoken')
  })
})
