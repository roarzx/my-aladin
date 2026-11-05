import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // 本地开发：后端默认监听 8080（见 dev_spec §9.5.1）
      '/issues': 'http://127.0.0.1:8080',
      '/workspaces': 'http://127.0.0.1:8080',
      '/plans': 'http://127.0.0.1:8080',
      '/steps': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
})
