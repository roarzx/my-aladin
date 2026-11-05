import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import IssueCockpit from './pages/IssueCockpit'

const taskId = new URLSearchParams(window.location.search).get('taskId') ?? ''
const root = document.getElementById('root')
if (!root) throw new Error('缺少 #root 挂载点')

createRoot(root).render(
  <StrictMode>
    {taskId ? (
      <IssueCockpit taskId={taskId} />
    ) : (
      <main>
        <h1>阿拉丁</h1>
        <p>打开 /?taskId=&lt;任务 ID&gt; 进入 Issue 驾驶舱。</p>
      </main>
    )}
  </StrictMode>,
)
