import AppShell from '../components/AppShell'
import { toast } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   下载桌面版
   对照 hercules-frontend /desktop/download。
   本地 Go 后端未提供安装包发布接口，下载入口只提示，不伪造安装包。
   ------------------------------------------------------------------ */

interface BuildRow {
  key: string
  label: string
  requirement: string
}

const MAC_BUILDS: BuildRow[] = [
  { key: 'arm64', label: 'Apple 芯片版', requirement: '适用于 M1、M2、M3、M4 等 Apple 芯片' },
  { key: 'x64', label: 'Intel 芯片版', requirement: '适用于 Intel 处理器 Mac' },
]

const WINDOWS_BUILDS: BuildRow[] = [
  { key: 'x64', label: 'Windows 版', requirement: 'Windows 10 及以上版本' },
]

const INSTALL_STEPS = [
  '下载与系统匹配的安装包：Apple 芯片 Mac 选择 Apple 芯片版，Intel Mac 选择 Intel 芯片版。',
  '打开安装包，将阿拉丁拖入「应用程序」目录后完成安装。',
  '首次启动如提示“无法验证开发者”，请在「系统设置 - 隐私与安全性」中允许打开。',
  '启动后使用访问令牌登录，即可连接当前工作区。',
]

function BuildList({ builds }: { builds: BuildRow[] }) {
  return (
    <div className="desktop-build-list">
      {builds.map((build) => (
        <div className="desktop-build-row" key={build.key}>
          <div className="build-copy">
            <strong>{build.label}</strong>
            <span>{build.requirement}</span>
          </div>
          <div className="desktop-download-empty">
            <strong>暂无可用版本</strong>
            <span>新版本发布后会显示在这里</span>
          </div>
          <button
            type="button"
            className="button ghost small"
            onClick={() => toast('本地后端尚未提供该能力')}
          >
            下载
          </button>
        </div>
      ))}
    </div>
  )
}

export default function DesktopDownload() {
  return (
    <AppShell title="下载桌面版" breadcrumb="桌面版 / 下载">
      <div className="desktop-page-stack">
        <section className="desktop-download-hero">
          <span className="desktop-eyebrow">HERCULES DESKTOP</span>
          <h1>阿拉丁 桌面应用</h1>
          <p>在 macOS 或 Windows 上运行阿拉丁 AI 研发控制台，随时连接工作区、任务与 Agent。</p>
          <div className="desktop-hero-chips">
            <span className="chip">macOS 12 及以上版本</span>
            <span className="chip">Windows 10 及以上版本</span>
          </div>
        </section>

        <div className="desktop-download-grid">
          <section className="desktop-download-card" aria-label="macOS 下载">
            <div className="desktop-platform-head">
              <div className="desktop-platform-icon" aria-hidden="true">
                ⌘
              </div>
              <div>
                <h2>macOS</h2>
                <p>macOS 12 及以上版本</p>
              </div>
            </div>
            <BuildList builds={MAC_BUILDS} />
          </section>

          <section className="desktop-download-card" aria-label="Windows 下载">
            <div className="desktop-platform-head">
              <div className="desktop-platform-icon" aria-hidden="true">
                ⊞
              </div>
              <div>
                <h2>Windows</h2>
                <p>Windows 10 及以上版本</p>
              </div>
            </div>
            <BuildList builds={WINDOWS_BUILDS} />
          </section>
        </div>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>安装步骤</h2>
              <p>安装包发布后，按以下步骤完成安装并登录。</p>
            </div>
          </div>
          <ol className="desktop-step-list">
            {INSTALL_STEPS.map((step) => (
              <li key={step}>{step}</li>
            ))}
          </ol>
        </section>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>版本说明</h2>
              <p>桌面版与网页端共用同一套账号与工作区数据，升级后无需重新配置。</p>
            </div>
          </div>
          <p className="notice warn">本地后端尚未提供该能力，下载入口与安装包均不会实际生效。</p>
        </section>
      </div>
    </AppShell>
  )
}
