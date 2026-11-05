import { useState } from 'react'

import IssueList from './pages/IssueList'
import IssueCockpit from './pages/IssueCockpit'
import TokenGate from './pages/TokenGate'
import { currentToken } from './api/issues'

export default function App() {
  const [token, setToken] = useState<string>(() => currentToken())
  const taskId = new URLSearchParams(window.location.search).get('taskId') ?? ''

  if (!token) {
    return (
      <TokenGate
        onSave={(value) => {
          localStorage.setItem('aladdin_token', value)
          setToken(value)
        }}
      />
    )
  }

  return taskId ? <IssueCockpit taskId={taskId} /> : <IssueList />
}
