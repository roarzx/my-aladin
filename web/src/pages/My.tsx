import AppShell from '../components/AppShell'
import { EmptyState } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   我的
   对照 hercules-frontend 的个人资料页。
   本地后端未提供账号接口，展示当前本地调试账户的静态信息。
   ------------------------------------------------------------------ */

const PROFILE_ITEMS = [
  { label: '账号', value: '本地调试账户' },
  { label: '角色', value: 'Owner' },
  { label: '工作区', value: '个人工作区' },
  { label: '登录方式', value: 'Bearer 令牌' },
]

export default function My() {
  return (
    <AppShell title="我的" breadcrumb="我的">
      <div className="settings-stack">
        <section className="panel">
          <div className="settings-panel-body profile-panel-body">
            <div className="profile-hero">
              <div className="profile-avatar" aria-hidden="true">
                本
              </div>
              <div className="profile-identity">
                <h2>本地调试账户</h2>
                <p>通过 Bearer 令牌访问本地后端控制台。</p>
                <div className="profile-meta">
                  <span className="chip">个人工作区</span>
                  <span className="chip">Owner</span>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>账号信息</h2>
              <p>当前账号的基本信息与访问方式。</p>
            </div>
          </div>
          <div className="settings-panel-body">
            <div className="profile-grid">
              {PROFILE_ITEMS.map((item) => (
                <div className="profile-item" key={item.label}>
                  <span className="label">{item.label}</span>
                  <span className="value">{item.value}</span>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>快捷入口</h2>
              <p>常用页面入口。</p>
            </div>
          </div>
          <div className="settings-panel-body">
            <div className="profile-links">
              <Link to="/settings" className="button ghost">
                系统设置
              </Link>
              <Link to="/desktop/download" className="button ghost">
                下载桌面版
              </Link>
              <Link to="/desktop/releases" className="button ghost">
                版本发布
              </Link>
            </div>
          </div>
        </section>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>最近动态</h2>
              <p>你在工作区内的近期操作会展示在这里。</p>
            </div>
          </div>
          <div className="empty-inline dependency-admin-empty">
            <EmptyState
              title="暂无最近动态"
              description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可展示动态。"
            />
          </div>
        </section>
      </div>
    </AppShell>
  )
}
