import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('机器注册：显示一次性 token 与 aladdin daemon start 命令', async ({ page }) => {
  await login(page)
  await page.goto('/machines')
  await page.getByRole('button', { name: '注册机器' }).click()

  const name = `E2E 机器 ${Date.now()}`
  await page.getByLabel('机器名称').fill(name)
  await page.getByRole('button', { name: '注册', exact: true }).click()

  await expect(page.getByText('机器注册成功')).toBeVisible()
  await expect(page.getByText('机器密钥仅展示一次')).toBeVisible()
  await expect(page.getByText(/aladdin daemon start/)).toBeVisible()
  await expect(page.getByText(/--server-auth-token/)).toBeVisible()
  await expect(page.getByText(/^[a-f0-9]{64}$/)).toBeVisible()

  await page.getByRole('button', { name: '完成' }).click()
  await expect(page.getByText(name, { exact: false })).toBeVisible()
})
