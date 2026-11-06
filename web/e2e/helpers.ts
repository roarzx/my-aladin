import fs from 'node:fs'
import path from 'node:path'
import type { APIRequestContext, Page } from '@playwright/test'

export const API = 'http://127.0.0.1:8080'
export const AUTH = { Authorization: 'Bearer devtoken' }

export interface E2EState {
  workspaceId: string
  repoUrl: string
  fakeAgent: string
  repoRoot: string
}

export function state(): E2EState {
  const file = path.join(process.cwd(), 'e2e', '.e2e-state.json')
  return JSON.parse(fs.readFileSync(file, 'utf8')) as E2EState
}

export async function api<T = any>(
  request: APIRequestContext,
  method: 'GET' | 'POST' | 'PATCH' | 'DELETE',
  pathname: string,
  data?: unknown,
): Promise<T> {
  const response = await request.fetch(`${API}${pathname}`, {
    method,
    headers: { ...AUTH, 'Content-Type': 'application/json' },
    data,
  })
  if (!response.ok()) {
    throw new Error(`${method} ${pathname} -> ${response.status()}: ${await response.text()}`)
  }
  return (await response.json()).data as T
}

export async function login(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('aladdin_token', 'devtoken')
  })
}

export async function waitMachineOnline(
  request: APIRequestContext,
  workspaceId: string,
  machineId: string,
  timeoutMs = 30_000,
) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const machines = await api<any[]>(request, 'GET', `/machines?workspaceId=${workspaceId}`)
    const machine = machines.find((item) => item.id === machineId)
    if (machine?.status === 'online') return
    if (Date.now() > deadline) {
      throw new Error(`机器未在 ${timeoutMs}ms 内上线: ${machineId}`)
    }
    await new Promise((resolve) => setTimeout(resolve, 300))
  }
}
