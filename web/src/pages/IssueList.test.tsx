import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

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
    render(<IssueList />)

    const link = await screen.findByRole('link', { name: '演示任务' })
    expect(link).toHaveAttribute('href', '/?taskId=task-1')
    expect(screen.getByText('对话中')).toBeInTheDocument()
  })

  it('空列表给出创建提示', async () => {
    stubFetch({ data: [] })
    render(<IssueList />)
    expect(await screen.findByText(/还没有任务/)).toBeInTheDocument()
  })

  it('接口失败显示错误信息', async () => {
    stubFetch({ error: { code: 'UNAUTHENTICATED', message: '缺少 Bearer 令牌' } }, 401)
    render(<IssueList />)
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
