import { expect, test } from '@playwright/test'
import { api, login, state } from './helpers'

test('Agent 配置：绑定机器、工作目录与 System Prompt', async ({ page, request }) => {
  const env = state()
  const machine = await api<any>(request, 'POST', '/machines', {
    workspaceId: env.workspaceId,
    name: `E2E Agent 机器 ${Date.now()}`,
    kind: 'local',
  })
  const agentName = `E2E Agent ${Date.now()}`
  const workDir = `/tmp/aladdin-playwright/agent-${Date.now()}`

  await login(page)
  await page.goto('/agents')
  await page.getByRole('button', { name: '创建 Agent' }).click()
  await page.getByLabel('Agent 名称').fill(agentName)
  await page.getByLabel('绑定机器').selectOption(machine.machine.id)
  await page.getByLabel('工作目录').fill(workDir)
  await page.getByLabel('System Prompt').fill('你是 Playwright E2E Agent。')
  await page.getByRole('button', { name: '确认创建' }).click()

  await expect(page.getByText(`已创建 Agent：${agentName}`)).toBeVisible()
  await expect(page.getByText(agentName, { exact: false }).first()).toBeVisible()
  await expect(page.getByText(workDir, { exact: false }).first()).toBeVisible()
})
