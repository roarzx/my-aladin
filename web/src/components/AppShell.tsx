import { useEffect, useState, type ReactNode } from 'react'

import { Link, useRouter } from '../router'
import {
  IconAutomation,
  IconBox,
  IconChat,
  IconChevronDown,
  IconClose,
  IconHome,
  IconIssue,
  IconLayers,
  IconLogout,
  IconMenu,
  IconSend,
  IconSettings,
  IconUser,
} from './icons'
import { ToastHost, toast } from './ui'

const COLLAPSE_KEY = 'hercules_sidebar_collapsed'

interface NavItem {
  to: string
  label: string
}

interface NavGroup {
  key: string
  label: string
  icon: ReactNode
  items: NavItem[]
}

const TOP_ITEMS: Array<NavItem & { icon: ReactNode }> = [
  { to: '/', label: '首页', icon: <IconHome /> },
  { to: '/task/list', label: '我的 Issue', icon: <IconIssue /> },
  { to: '/conversations', label: '对话', icon: <IconChat /> },
  { to: '/task-schedules', label: '自动化', icon: <IconAutomation /> },
]

const GROUPS: NavGroup[] = [
  {
    key: 'dependency',
    label: '依赖管理',
    icon: <IconBox />,
    items: [
      { to: '/dependencies/knowledge-bases', label: '知识库管理' },
      { to: '/dependencies/skills', label: 'Skills 管理' },
      { to: '/dependencies/mcps', label: 'MCP 管理' },
      { to: '/dependencies/plugins', label: '插件市场' },
    ],
  },
  {
    key: 'resource',
    label: '资源管理',
    icon: <IconLayers />,
    items: [
      { to: '/machines', label: '机器管理' },
      { to: '/agents', label: 'Agent 管理' },
      { to: '/resources/squads', label: '小队' },
      { to: '/resources/templates', label: '项目管理' },
      { to: '/resources/flows', label: '流程管理' },
      { to: '/webhooks', label: 'Webhook' },
    ],
  },
  {
    key: 'channel',
    label: '渠道',
    icon: <IconSend />,
    items: [
      { to: '/channel-bots', label: '渠道机器人' },
      { to: '/channel-feedback', label: '渠道反馈' },
    ],
  },
]

const BOTTOM_ITEM: NavItem & { icon: ReactNode } = {
  to: '/settings',
  label: '设置',
  icon: <IconSettings />,
}

function isActive(pathname: string, to: string): boolean {
  if (to === '/') return pathname === '/'
  return pathname === to || pathname.startsWith(`${to}/`)
}

function Brand({ collapsed }: { collapsed?: boolean }) {
  return (
    <div className="brand">
      <div className="brand-mark" aria-hidden>
        阿
      </div>
      {collapsed ? null : (
        <div className="brand-copy">
          <div className="brand-name">阿拉丁</div>
          <div className="brand-subtitle">AI 研发控制台</div>
        </div>
      )}
    </div>
  )
}

function Navigation({
  pathname,
  collapsed,
  expanded,
  onToggleGroup,
  onNavigate,
}: {
  pathname: string
  collapsed: boolean
  expanded: Record<string, boolean>
  onToggleGroup: (key: string) => void
  onNavigate?: () => void
}) {
  const showLabels = !collapsed
  return (
    <nav className="nav">
      {TOP_ITEMS.map((item) => (
        <Link
          key={item.to}
          to={item.to}
          exact
          className="nav-item"
          activeClassName="active"
          onClick={onNavigate}
        >
          <span className="icon">{item.icon}</span>
          {showLabels ? <span className="nav-label">{item.label}</span> : null}
        </Link>
      ))}

      {GROUPS.map((group) => {
        const groupActive = group.items.some((item) => isActive(pathname, item.to))
        const isExpanded = expanded[group.key]
        return (
          <div className="nav-group" key={group.key}>
            <button
              type="button"
              className={`nav-item nav-group-trigger ${groupActive ? 'current' : ''}`}
              aria-expanded={isExpanded}
              onClick={() => onToggleGroup(group.key)}
            >
              <span className="icon">{group.icon}</span>
              {showLabels ? (
                <>
                  <span className="nav-label">{group.label}</span>
                  <span className={`nav-caret ${isExpanded ? 'expanded' : ''}`}>
                    <IconChevronDown width={14} height={14} />
                  </span>
                </>
              ) : null}
            </button>
            {showLabels && isExpanded ? (
              <div className="nav-subitems">
                {group.items.map((item) => (
                  <Link
                    key={item.to}
                    to={item.to}
                    className="nav-subitem"
                    activeClassName="active"
                    onClick={onNavigate}
                  >
                    {item.label}
                  </Link>
                ))}
              </div>
            ) : null}
          </div>
        )
      })}

      <Link
        to={BOTTOM_ITEM.to}
        className="nav-item"
        activeClassName="active"
        onClick={onNavigate}
        style={{ marginTop: 8 }}
      >
        <span className="icon">{BOTTOM_ITEM.icon}</span>
        {showLabels ? <span className="nav-label">{BOTTOM_ITEM.label}</span> : null}
      </Link>
    </nav>
  )
}

function UserMenu() {
  const [open, setOpen] = useState(false)

  const switchToken = () => {
    localStorage.removeItem('aladdin_token')
    window.location.reload()
  }

  return (
    <div className="user-menu-wrap">
      <button type="button" className="button ghost" onClick={() => setOpen((next) => !next)}>
        <IconUser width={16} height={16} /> 本机账户
      </button>
      {open ? (
        <>
          <button
            type="button"
            className="menu-backdrop"
            aria-label="关闭菜单"
            onClick={() => setOpen(false)}
          />
          <div className="user-menu">
            <div className="user-menu-head">
              <div className="user-avatar">阿</div>
              <div className="user-menu-meta">
                <strong>本地调试账户</strong>
                <span>Bearer 令牌访问</span>
              </div>
            </div>
            <div className="user-menu-body">
              <Link className="user-menu-link" to="/my" onClick={() => setOpen(false)}>
                <IconUser width={15} height={15} /> 我的
              </Link>
              <button type="button" className="user-menu-link" onClick={switchToken}>
                <IconLogout width={15} height={15} /> 切换令牌
              </button>
              <button
                type="button"
                className="user-menu-link danger"
                onClick={() => {
                  toast('已退出登录')
                  switchToken()
                }}
              >
                <IconLogout width={15} height={15} /> 退出登录
              </button>
            </div>
          </div>
        </>
      ) : null}
    </div>
  )
}

export interface AppShellProps {
  title: ReactNode
  breadcrumb?: ReactNode
  actions?: ReactNode
  children: ReactNode
  /** 额外的页面级 class（如 cockpit）。 */
  className?: string
}

export default function AppShell({ title, breadcrumb, actions, children, className }: AppShellProps) {
  const { pathname } = useRouter()
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSE_KEY) === '1'
    } catch {
      return false
    }
  })
  const [mobileOpen, setMobileOpen] = useState(false)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  // 进入子页面时自动展开所属分组
  useEffect(() => {
    setExpanded((prev) => {
      const next = { ...prev }
      for (const group of GROUPS) {
        if (group.items.some((item) => isActive(pathname, item.to))) next[group.key] = true
      }
      return next
    })
  }, [pathname])

  useEffect(() => {
    try {
      localStorage.setItem(COLLAPSE_KEY, collapsed ? '1' : '0')
    } catch {
      /* 忽略存储异常 */
    }
  }, [collapsed])

  useEffect(() => {
    document.title = `${typeof title === 'string' ? title : '阿拉丁'} - 阿拉丁`
  }, [title])

  const toggleGroup = (key: string) =>
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }))

  const nav = (
    <Navigation
      pathname={pathname}
      collapsed={false}
      expanded={expanded}
      onToggleGroup={toggleGroup}
      onNavigate={() => setMobileOpen(false)}
    />
  )

  return (
    <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`}>
      <aside className="sidebar">
        <div className="sidebar-header">
          <Brand collapsed={collapsed} />
        </div>
        <Navigation
          pathname={pathname}
          collapsed={collapsed}
          expanded={expanded}
          onToggleGroup={toggleGroup}
        />
        <div className="sidebar-bottom">
          <button
            type="button"
            className="sidebar-toggle sidebar-toggle-bottom"
            aria-label={collapsed ? '展开菜单栏' : '收起菜单栏'}
            onClick={() => setCollapsed((prev) => !prev)}
          >
            {collapsed ? '›' : '‹'}
          </button>
        </div>
      </aside>

      <main className={`main ${className ?? ''}`}>
        <header className="topbar">
          <div className="topbar-title">
            <button
              type="button"
              className="mobile-nav-trigger"
              aria-label="打开菜单"
              onClick={() => setMobileOpen(true)}
            >
              <IconMenu width={18} height={18} />
            </button>
            <div>
              {breadcrumb ? <div className="breadcrumb">{breadcrumb}</div> : null}
              {title ? <h1>{title}</h1> : null}
            </div>
          </div>
          <div className="topbar-actions">
            {actions}
            <UserMenu />
          </div>
        </header>
        <div className="main-content">{children}</div>
      </main>

      {mobileOpen ? (
        <div className="mobile-nav-drawer" role="dialog" aria-label="导航菜单">
          <button
            type="button"
            className="mobile-nav-drawer-backdrop"
            aria-label="关闭菜单"
            onClick={() => setMobileOpen(false)}
          />
          <div className="mobile-nav-drawer-panel">
            <div className="row-between">
              <Brand />
              <button
                type="button"
                className="sidebar-toggle"
                aria-label="关闭菜单"
                onClick={() => setMobileOpen(false)}
                style={{ width: 32 }}
              >
                <IconClose width={14} height={14} />
              </button>
            </div>
            {nav}
          </div>
        </div>
      ) : null}

      <ToastHost />
    </div>
  )
}
