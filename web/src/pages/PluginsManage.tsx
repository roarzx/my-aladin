import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconSearch } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   插件管理
   对照 hercules-frontend /dependencies/plugins/manage。
   本地 Go 后端未提供插件接口，列表恒为空，不伪造数据。
   ------------------------------------------------------------------ */

const STATUS_FILTERS = [
  { label: '全部', value: 'all' },
  { label: '已启用', value: 'enabled' },
  { label: '已停用', value: 'disabled' },
] as const

type StatusFilter = (typeof STATUS_FILTERS)[number]['value']

interface ManagedPlugin {
  id: string
  name: string
  version: string
  source: string
  enabled: boolean
  updatedAt: string
}

/** 本地后端尚未提供插件能力，接口接入前列表恒为空。 */
const ROWS: ManagedPlugin[] = []

function DeletePluginConfirm({ name, onClose }: { name: string; onClose: () => void }) {
  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="删除插件">
        <div className="modal-head">
          <h3>删除插件</h3>
        </div>
        <div className="modal-body">
          <p>确认删除“{name}”？</p>
          <p className="notice warn">本地后端尚未提供该能力，确认后不会真正删除。</p>
        </div>
        <div className="modal-foot">
          <button type="button" className="button subtle" onClick={onClose}>
            取消
          </button>
          <button type="button" className="button danger" onClick={onClose}>
            删除
          </button>
        </div>
      </div>
    </div>
  )
}

export default function PluginsManage() {
  const [status, setStatus] = useState<StatusFilter>('all')
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [pendingDelete, setPendingDelete] = useState<ManagedPlugin | null>(null)

  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase()
    return ROWS.filter((row) => {
      if (status === 'enabled' && !row.enabled) return false
      if (status === 'disabled' && row.enabled) return false
      if (!needle) return true
      return (
        row.name.toLowerCase().includes(needle) ||
        row.source.toLowerCase().includes(needle)
      )
    })
  }, [status, keyword])

  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    setKeyword(searchInput)
  }

  return (
    <AppShell title="插件管理" breadcrumb="依赖管理 / 插件 / 管理">
      <section className="panel dependency-admin-panel">
        <div className="panel-head">
          <div>
            <h2>插件管理</h2>
            <p>管理当前工作区已安装的插件，可在此启用、停用或删除。</p>
          </div>
        </div>

        <div className="toolbar dependency-admin-toolbar">
          <div className="filters">
            {STATUS_FILTERS.map((item) => (
              <button
                key={item.value}
                type="button"
                className={`filter ${status === item.value ? 'active' : ''}`}
                onClick={() => setStatus(item.value)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <form className="dependency-admin-search" onSubmit={submitSearch}>
            <IconSearch width={15} height={15} />
            <input
              value={searchInput}
              placeholder="搜索插件名称或来源"
              aria-label="搜索插件名称或来源"
              onChange={(event) => setSearchInput(event.target.value)}
            />
          </form>
        </div>

        <div className="dependency-admin-table cols-5">
          <div className="dependency-admin-table-head">
            <span>名称</span>
            <span>版本</span>
            <span>来源</span>
            <span>启用状态</span>
            <span>更新时间 / 操作</span>
          </div>

          {filtered.length === 0 ? (
            <div className="empty-inline dependency-admin-empty">
              <EmptyState
                title="暂无已安装的插件。"
                description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可展示数据。"
              />
            </div>
          ) : (
            filtered.map((row) => (
              <article className="dependency-admin-table-row" key={row.id}>
                <span className="dependency-admin-cell dependency-admin-cell-name">
                  <strong>{row.name}</strong>
                </span>
                <span className="dependency-admin-cell">
                  <span className="chip">{row.version}</span>
                </span>
                <span className="dependency-admin-cell">{row.source}</span>
                <span className="dependency-admin-cell">
                  <StateTag tone={row.enabled ? 'running' : 'done'}>
                    {row.enabled ? '已启用' : '已停用'}
                  </StateTag>
                </span>
                <span className="dependency-admin-cell dependency-admin-cell-actions">
                  <span className="dependency-admin-cell-time">{row.updatedAt}</span>
                  <button
                    type="button"
                    className="dependency-admin-action"
                    onClick={() => toast('本地后端尚未提供该能力')}
                  >
                    {row.enabled ? '停用' : '启用'}
                  </button>
                  <span className="dependency-admin-action-divider">|</span>
                  <button
                    type="button"
                    className="dependency-admin-action danger"
                    onClick={() => setPendingDelete(row)}
                  >
                    删除
                  </button>
                </span>
              </article>
            ))
          )}
        </div>

        <div className="dependency-admin-pagination">
          <span>共 {filtered.length} 条</span>
          <div className="dependency-admin-pagination-actions">
            <button type="button" className="dependency-admin-page-button" disabled>
              上一页
            </button>
            <button type="button" className="dependency-admin-page-button" disabled>
              下一页
            </button>
          </div>
        </div>
      </section>

      {pendingDelete ? (
        <DeletePluginConfirm name={pendingDelete.name} onClose={() => setPendingDelete(null)} />
      ) : null}
    </AppShell>
  )
}
