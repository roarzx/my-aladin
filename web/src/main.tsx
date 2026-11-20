import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from './App'
import { applyTheme, storedTheme } from './theme'
import './styles.css'

// 在首次渲染前定好主题，避免闪一下白底。
applyTheme(storedTheme())

const root = document.getElementById('root')
if (!root) throw new Error('缺少 #root 挂载点')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
