import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// 页面路由与 API 前缀重名（/machines、/agents）：浏览器直接访问时返回 SPA，
// fetch/XHR 仍代理到后端。
const spaBypass = (req: { headers: Record<string, string | string[] | undefined> }) => {
  const accept = req.headers.accept
  const value = Array.isArray(accept) ? accept.join(',') : accept ?? ''
  return value.includes('text/html') ? '/index.html' : undefined
}

const apiTarget = {
  target: 'http://127.0.0.1:8080',
  bypass: spaBypass,
}

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // 本地开发：后端默认监听 8080（见 dev_spec §9.5.1）
      '/issues': apiTarget,
      '/workspaces': apiTarget,
      '/plans': apiTarget,
      '/steps': apiTarget,
      '/machines': apiTarget,
      '/agents': apiTarget,
      '/repositories': apiTarget,
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
})
