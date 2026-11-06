import { spawn, type ChildProcess } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { expect, test } from '@playwright/test'
import { api, login, state, waitMachineOnline } from './helpers'

let daemon: ChildProcess | undefined

test.afterAll(async () => {
  if (!daemon) return
  daemon.kill('SIGTERM')
  await new Promise<void>((resolve) => {
    if (daemon!.exitCode !== null) return resolve()
    daemon!.once('exit', () => resolve())
    setTimeout(resolve, 5000)
  })
})

test('主路径：创建 Agent/Issue → 计划启动 → fake 执行 → 本地 PR 链接', async ({ page, request }) => {
  const env = state()
  const stamp = Date.now()
  const machine = await api<any>(request, 'POST', '/machines', {
    workspaceId: env.workspaceId,
    name: `E2E 主路径机器 ${stamp}`,
    kind: 'local',
  })
  const repo = await api<any>(request, 'POST', '/repositories', {
    workspaceId: env.workspaceId,
    url: env.repoUrl,
    defaultBranch: 'main',
  })

  fs.rmSync('/tmp/aladdin-playwright/profiles', { recursive: true, force: true })
  daemon = spawn(path.join(env.repoRoot, 'bin', 'aladdin'), [
    'daemon', 'start',
    '--machine-id', machine.machine.id,
    '--server-auth-token', machine.token,
    '--config', path.join(process.cwd(), 'e2e', 'fixtures', 'daemon.yaml'),
  ], {
    cwd: env.repoRoot,
    env: { ...process.env, ALADDIN_FAKE_AGENT: env.fakeAgent, ALADDIN_PR_PROVIDER: 'local' },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  daemon.stdout?.pipe(process.stdout)
  daemon.stderr?.pipe(process.stderr)
  await waitMachineOnline(request, env.workspaceId, machine.machine.id)

  const agentName = `E2E 主路径 Agent ${stamp}`
  const workDir = `/tmp/aladdin-playwright/work-${stamp}`
  await login(page)
  await page.goto('/agents')
  await page.getByRole('button', { name: '创建 Agent' }).click()
  await page.getByLabel('Agent 名称').fill(agentName)
  await page.getByLabel('绑定机器').selectOption(machine.machine.id)
  await page.getByLabel('工作目录').fill(workDir)
  await page.getByLabel('System Prompt').fill('你是 Playwright E2E Agent。')
  await page.getByRole('button', { name: '确认创建' }).click()
  await expect(page.getByText(`已创建 Agent：${agentName}`)).toBeVisible()

  const agents = await api<any[]>(request, 'GET', `/agents?workspaceId=${env.workspaceId}`)
  const agent = agents.find((item) => item.name === agentName)
  expect(agent).toBeTruthy()

  const taskName = `E2E 主路径任务 ${stamp}`
  await page.goto('/task/create')
  await page.getByLabel('任务名称').fill(taskName)
  await page.getByLabel('需求描述').fill('在 README.md 末尾追加一行：Playwright E2E。只修改 README.md。')
  await page.getByRole('button', { name: '下一步：选择 Agent 与仓库' }).click()
  await page.getByLabel('执行 Agent').selectOption(agent.id)
  await page.getByLabel('仓库').selectOption(repo.id)
  await page.getByRole('button', { name: '创建任务并进入驾驶舱' }).click()

  await expect(page).toHaveURL(/\/task\/detail\?id=/)
  await expect(page.getByRole('heading', { name: taskName })).toBeVisible()

  await page.getByRole('button', { name: '提交计划' }).click()
  await page.getByLabel('第 1 步标题').fill('修改 README')
  await page.getByLabel('第 1 步说明').fill('在 README.md 末尾追加一行：Playwright E2E。只修改 README.md。')
  await page.getByRole('button', { name: '提交计划' }).click()
  await page.getByRole('button', { name: '确认并启动' }).click()

  await expect(page.getByText('对话中', { exact: false }).first()).toBeVisible({ timeout: 60_000 })
  await expect(page.getByText('step.done')).toBeVisible()
  const prLink = page.getByRole('link', { name: '查看 PR' })
  await expect(prLink).toBeVisible()
  await expect(prLink).toHaveAttribute('href', /^local:\/\/pull\//)

  const taskId = new URL(page.url()).searchParams.get('id')!
  const detail = await api<any>(request, 'GET', `/issues/${taskId}`)
  expect(detail.task.status).toBe('chatting')
  const events = await api<any>(request, 'GET', `/issues/${taskId}/events?after=0`)
  const done = events.find((event: any) => event.kind === 'step.done')
  expect(done?.payload?.commit_sha).toBeTruthy()
})
