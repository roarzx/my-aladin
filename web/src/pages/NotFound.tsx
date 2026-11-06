import AppShell from '../components/AppShell'
import { EmptyState, Panel } from '../components/ui'
import { Link } from '../router'

export default function NotFound() {
  return (
    <AppShell title="页面不存在" breadcrumb="404">
      <Panel title="页面不存在">
        <EmptyState
          title="404"
          description="你访问的页面不存在或已被移动。"
          action={
            <Link className="button primary" to="/">
              返回首页
            </Link>
          }
        />
      </Panel>
    </AppShell>
  )
}
