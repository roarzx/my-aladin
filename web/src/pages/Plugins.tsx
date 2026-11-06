import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, toast } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   插件市场
   对照 hercules-frontend /dependencies/plugins。
   本地 Go 后端未提供插件接口，列表恒为空，不伪造数据。
   ------------------------------------------------------------------ */

const MARKET_FILTERS = [
  { label: '全部插件', value: 'market' },
  { label: '已安装', value: 'installed' },
] as const

type MarketFilter = (typeof MARKET_FILTERS)[number]['value']

const PAGE_SIZE = 12

interface PluginRow {
  id: string
  name: string
  displayName: string
  logo: string
  version: string
  description: string
  author: string
  capabilities: string[]
  installed: boolean
  published: boolean
}

/** 本地后端尚未提供插件能力，接口接入前列表恒为空。 */
const PLUGINS: PluginRow[] = []

export default function Plugins() {
  const [filter, setFilter] = useState<MarketFilter>('market')
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)

  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase()
    return PLUGINS.filter((plugin) => {
      if (filter === 'installed' && !plugin.installed) return false
      if (!needle) return true
      return (
        plugin.displayName.toLowerCase().includes(needle) ||
        plugin.name.toLowerCase().includes(needle) ||
        plugin.description.toLowerCase().includes(needle)
      )
    })
  }, [filter, keyword])

  const total = filtered.length
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const current = Math.min(page, pages)
  const visible = filtered.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE)

  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    setKeyword(searchInput)
    setPage(1)
  }

  const install = () => toast('本地后端尚未提供该能力')

  return (
    <AppShell title="插件市场" breadcrumb="依赖管理 / 插件">
      <section className="panel plugin-market-panel">
        <div className="panel-head">
          <div>
            <h2>插件市场</h2>
            <p>浏览工作区可用的插件，安装后即可连接对应的 MCP 与 Skill 能力。</p>
          </div>
          <div className="panel-actions dependency-admin-action-group">
            <button type="button" className="button primary" onClick={install}>
              <IconPlus width={15} height={15} /> 发布插件
            </button>
          </div>
        </div>

        <div className="toolbar dependency-admin-toolbar">
          <div className="filters">
            {MARKET_FILTERS.map((item) => (
              <button
                key={item.value}
                type="button"
                className={`filter ${filter === item.value ? 'active' : ''}`}
                onClick={() => {
                  setFilter(item.value)
                  setPage(1)
                }}
              >
                {item.label}
              </button>
            ))}
          </div>
          <form className="dependency-admin-search" onSubmit={submitSearch}>
            <IconSearch width={15} height={15} />
            <input
              value={searchInput}
              placeholder="搜索插件名称或描述"
              aria-label="搜索插件名称或描述"
              onChange={(event) => setSearchInput(event.target.value)}
            />
          </form>
        </div>

        {visible.length === 0 ? (
          <div className="empty-inline dependency-admin-empty">
            <EmptyState
              title="暂无插件"
              description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可展示插件。"
            />
          </div>
        ) : (
          <div className="plugin-market-grid">
            {visible.map((plugin) => (
              <article className="plugin-card" key={plugin.id}>
                <div className="plugin-card-title">
                  <div className="plugin-card-logo">{plugin.logo || 'P'}</div>
                  <span className="plugin-card-name">{plugin.displayName || plugin.name}</span>
                  <span className="chip">{plugin.version}</span>
                  {plugin.installed ? <span className="chip">已安装</span> : null}
                  {plugin.published ? null : <span className="chip">已下架</span>}
                </div>
                <p className="plugin-card-description">{plugin.description}</p>
                <div className="plugin-card-author">作者：{plugin.author}</div>
                <div className="plugin-card-capabilities">
                  {plugin.capabilities.map((capability) => (
                    <span className="chip" key={capability}>
                      {capability}
                    </span>
                  ))}
                </div>
                <div className="plugin-card-actions">
                  <button
                    type="button"
                    className="button primary small"
                    disabled={plugin.installed || !plugin.published}
                    onClick={install}
                  >
                    {plugin.installed ? '已安装' : '安装'}
                  </button>
                </div>
              </article>
            ))}
          </div>
        )}

        {total > PAGE_SIZE ? (
          <div className="dependency-admin-pagination">
            <span>
              共 {total} 条，第 {current} / {pages} 页
            </span>
            <div className="dependency-admin-pagination-actions">
              <button
                type="button"
                className="dependency-admin-page-button"
                disabled={current <= 1}
                onClick={() => setPage(current - 1)}
              >
                上一页
              </button>
              <button
                type="button"
                className="dependency-admin-page-button"
                disabled={current >= pages}
                onClick={() => setPage(current + 1)}
              >
                下一页
              </button>
            </div>
          </div>
        ) : null}
      </section>
    </AppShell>
  )
}
