import { useState, type ComponentType } from 'react'

import { RouterProvider, useRouter } from './router'
import { currentToken } from './api/issues'
import TokenGate from './pages/TokenGate'
import Dashboard from './pages/Dashboard'
import IssueList from './pages/IssueList'
import CreateTask from './pages/CreateTask'
import IssueCockpit from './pages/IssueCockpit'
import Conversations from './pages/Conversations'
import TaskSchedules from './pages/TaskSchedules'
import KnowledgeBases from './pages/KnowledgeBases'
import Skills from './pages/Skills'
import Mcps from './pages/Mcps'
import Plugins from './pages/Plugins'
import PluginsManage from './pages/PluginsManage'
import Machines from './pages/Machines'
import MachineDetail from './pages/MachineDetail'
import Agents from './pages/Agents'
import AgentDetail from './pages/AgentDetail'
import Squads from './pages/Squads'
import Templates from './pages/Templates'
import Flows from './pages/Flows'
import FlowRuns from './pages/FlowRuns'
import Webhooks from './pages/Webhooks'
import ChannelBots from './pages/ChannelBots'
import ChannelFeedback from './pages/ChannelFeedback'
import ChannelFeedbackDetail from './pages/ChannelFeedbackDetail'
import Settings from './pages/Settings'
import My from './pages/My'
import DesktopDownload from './pages/DesktopDownload'
import DesktopReleases from './pages/DesktopReleases'
import GlobalLogin from './pages/Login'
import NotFound from './pages/NotFound'

/** 路由表对照 hercules-frontend：http(s)://gateway.myscrm.cn/hercules-frontend/#/… */
const ROUTES: Record<string, ComponentType> = {
  '/': Dashboard,
  '/task/list': IssueList,
  '/task/create': CreateTask,
  '/conversations': Conversations,
  '/task-schedules': TaskSchedules,
  '/dependencies/knowledge-bases': KnowledgeBases,
  '/dependencies/skills': Skills,
  '/dependencies/mcps': Mcps,
  '/dependencies/plugins': Plugins,
  '/dependencies/plugins/manage': PluginsManage,
  '/machines': Machines,
  '/machine-detail': MachineDetail,
  '/agents': Agents,
  '/agent-detail': AgentDetail,
  '/resources/squads': Squads,
  '/resources/templates': Templates,
  '/resources/flows': Flows,
  '/resources/flow-runs': FlowRuns,
  '/webhooks': Webhooks,
  '/channel-bots': ChannelBots,
  '/channel-feedback': ChannelFeedback,
  '/channel-feedback/detail': ChannelFeedbackDetail,
  '/settings': Settings,
  '/my': My,
  '/desktop/download': DesktopDownload,
  '/desktop/releases': DesktopReleases,
  '/login': GlobalLogin,
}

function Routed() {
  const { pathname, query } = useRouter()

  if (pathname === '/task/detail') {
    const taskId = query.get('id') ?? query.get('taskId') ?? ''
    return <IssueCockpit taskId={taskId} />
  }

  const Page = ROUTES[pathname] ?? NotFound
  return <Page />
}

export default function App() {
  const [token, setToken] = useState<string>(() => currentToken())

  if (!token) {
    return (
      <RouterProvider>
        <TokenGate
          onSave={(value) => {
            localStorage.setItem('aladdin_token', value)
            setToken(value)
          }}
        />
      </RouterProvider>
    )
  }

  return (
    <RouterProvider>
      <Routed />
    </RouterProvider>
  )
}
