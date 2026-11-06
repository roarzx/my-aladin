import { defineConfig, devices } from '@playwright/test'

const isCI = !!process.env.CI
const ds = 'root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 20_000 },
  globalSetup: './e2e/global-setup.ts',
  reporter: isCI ? [['list'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL: 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: './bin/server',
      cwd: '..',
      url: 'http://127.0.0.1:8080/readyz',
      reuseExistingServer: !isCI,
      timeout: 120_000,
      env: {
        MYSQL_DSN: ds,
        REDIS_ADDR: '127.0.0.1:6379',
        ALADDIN_ADMIN_TOKEN: 'devtoken',
        ALADDIN_LOG_LEVEL: 'warn',
      },
    },
    {
      command: 'pnpm dev --host 127.0.0.1 --port 5173',
      url: 'http://127.0.0.1:5173/',
      reuseExistingServer: !isCI,
      timeout: 120_000,
      env: { VITE_DEV_TOKEN: 'devtoken' },
    },
  ],
})
