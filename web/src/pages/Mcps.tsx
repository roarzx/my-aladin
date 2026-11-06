import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, StateTag } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   MCP 管理
   对照 hercules-frontend 依赖管理下的 MCP 页面。
   本地 Go 后端未提供 MCP 接口，列表恒为空，不伪造数据。
   ------------------------------------------------------------------ */

const STATUS_FILTERS = [
  { label: '全部', value: 'all' },
  { label: '启用中', value: 'active' },
  { label: '已停用', value: 'inactive' },
] as const

type StatusFilter = (typeof STATUS_FILTERS)[number]['value']

const PAGE_SIZES = [10, 20, 50] as const
type PageSize = (typeof PAGE_SIZES)[number]

type AuthKind = 'none' | 'bearer' | 'header'

interface McpRow {
  id: string
  name: string
  address: string
  description: string
  auth: AuthKind
  status: 'active' | 'inactive'
  updatedAt: string
}

/** 本地后端尚未提供 MCP 能力，接口接入前列表恒为空。 */
const ROWS: McpRow[] = []

const AUTH_OPTIONS: Array<{ value: AuthKind; label: string; description: string }> = [
  { value: 'none', label: '无需认证', description: '公开可访问的 MCP 服务，连接时不附加认证信息。' },
  { value: 'bearer', label: 'Bearer Token', description: '在请求头中携带 Authorization: Bearer <token>。' },
  { value: 'header', label: '自定义 Header', description: '使用自定义请求头名称与取值完成认证。' },
]

interface Draft {
  name: string
  address: string
  description: string
  auth: AuthKind
  token: string
  headerName: string
  headerValue: string
  status: 'active' | 'inactive'
}

const EMPTY_DRAFT: Draft = {
  name: '',
  address: '',
  description: '',
  auth: 'none',
  token: '',
  headerName: '',
  headerValue: '',
  status: 'active',
}

function McpEditorModal({
  initial,
  onClose,
}: {
  initial?: McpRow
  onClose: () => void
}) {
  const [draft, setDraft] = useState<Draft>(() =>
    initial
      ? {
          name: initial.name,
          address: initial.address,
          description: initial.description,
          auth: initial.auth,
          token: '',
          headerName: '',
          headerValue: '',
          status: initial.status,
        }
      : EMPTY_DRAFT,
  )
  const [notice, setNotice] = useState('')

  const patch = (next: Partial<Draft>) => setDraft((prev) => ({ ...prev, ...next }))

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!draft.name.trim()) {
      setNotice('请填写 MCP 名称。')
      return
    }
    if (!draft.address.trim()) {
      setNotice('请填写 MCP 地址。')
      return
    }
    if (draft.auth === 'bearer' && !draft.token.trim()) {
      setNotice('请填写 Bearer Token。')
      return
    }
    if (draft.auth === 'header' && (!draft.headerName.trim() || !draft.headerValue.trim())) {
      setNotice('请填写自定义 Header 名称与取值。')
      return
    }
    setNotice('本地后端尚未提供该能力，保存不会写入数据库。')
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={initial ? '编辑 MCP' : '新建 MCP'}
      >
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>{initial ? '编辑 MCP' : '新建 MCP'}</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="dependency-form-grid">
              <label className="field wide">
                <span>名称</span>
                <input
                  value={draft.name}
                  placeholder="请输入 MCP 名称"
                  onChange={(event) => patch({ name: event.target.value })}
                />
              </label>

              <label className="field wide">
                <span>地址</span>
                <input
                  value={draft.address}
                  placeholder="例如 https://mcp.example.com/sse"
                  onChange={(event) => patch({ address: event.target.value })}
                />
              </label>

              <label className="field wide">
                <span>描述</span>
                <textarea
                  rows={3}
                  value={draft.description}
                  placeholder="描述该 MCP 提供的能力与适用场景"
                  onChange={(event) => patch({ description: event.target.value })}
                />
              </label>

              <div className="field wide">
                <span>认证方式</span>
                <div className="dependency-source-cards">
                  {AUTH_OPTIONS.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      className={`dependency-source-card ${draft.auth === option.value ? 'selected' : ''}`}
                      aria-pressed={draft.auth === option.value}
                      onClick={() => patch({ auth: option.value })}
                    >
                      <strong>{option.label}</strong>
                      <span>{option.description}</span>
                    </button>
                  ))}
                </div>
              </div>

              {draft.auth === 'bearer' ? (
                <label className="field wide">
                  <span>Bearer Token</span>
                  <input
                    type="password"
                    value={draft.token}
                    placeholder="请输入访问令牌"
                    onChange={(event) => patch({ token: event.target.value })}
                  />
                </label>
              ) : null}

              {draft.auth === 'header' ? (
                <>
                  <label className="field">
                    <span>Header 名称</span>
                    <input
                      value={draft.headerName}
                      placeholder="例如 X-API-Key"
                      onChange={(event) => patch({ headerName: event.target.value })}
                    />
                  </label>
                  <label className="field">
                    <span>Header 取值</span>
                    <input
                      type="password"
                      value={draft.headerValue}
                      placeholder="请输入 Header 取值"
                      onChange={(event) => patch({ headerValue: event.target.value })}
                    />
                  </label>
                </>
              ) : null}

              <label className="field wide">
                <span>状态</span>
                <select
                  value={draft.status}
                  onChange={(event) => patch({ status: event.target.value as Draft['status'] })}
                >
                  <option value="active">启用中</option>
                  <option value="inactive">已停用</option>
                </select>
              </label>

              {notice ? (
                <p className="notice warn dependency-form-notice" role="status">
                  {notice}
                </p>
              ) : null}
            </div>
          </div>
          <div className="modal-foot">
            <button type="button" className="button subtle" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="button primary">
              {initial ? '保存' : '创建'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function DeleteMcpConfirm({ name, onClose }: { name: string; onClose: () => void }) {
  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="删除 MCP">
        <div className="modal-head">
          <h3>删除 MCP</h3>
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

export default function Mcps() {
  const [status, setStatus] = useState<StatusFilter>('all')
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(10)
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<McpRow | null>(null)
  const [pendingDelete, setPendingDelete] = useState<McpRow | null>(null)

  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase()
    return ROWS.filter((row) => {
      if (status !== 'all' && row.status !== status) return false
      if (!needle) return true
      return (
        row.name.toLowerCase().includes(needle) ||
        row.address.toLowerCase().includes(needle) ||
        row.description.toLowerCase().includes(needle)
      )
    })
  }, [status, keyword])

  const total = filtered.length
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const current = Math.min(page, pages)

  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    setKeyword(searchInput)
    setPage(1)
  }

  return (
    <AppShell title="MCP 管理" breadcrumb="依赖管理 / MCP">
      <section className="panel dependency-admin-panel">
        <div className="panel-head">
          <div>
            <h2>MCP 管理</h2>
            <p>管理可供 Agent 引用的 MCP 服务，配置地址与认证方式后即可在任务中使用。</p>
          </div>
          <div className="panel-actions dependency-admin-action-group">
            <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
              <IconPlus width={15} height={15} /> 新建 MCP
            </button>
          </div>
        </div>

        <div className="toolbar dependency-admin-toolbar">
          <div className="filters">
            {STATUS_FILTERS.map((item) => (
              <button
                key={item.value}
                type="button"
                className={`filter ${status === item.value ? 'active' : ''}`}
                onClick={() => {
                  setStatus(item.value)
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
              placeholder="搜索 MCP 名称、地址或描述"
              aria-label="搜索 MCP 名称、地址或描述"
              onChange={(event) => setSearchInput(event.target.value)}
            />
          </form>
        </div>

        <div className="dependency-admin-table cols-6">
          <div className="dependency-admin-table-head">
            <span>名称</span>
            <span>地址</span>
            <span>描述</span>
            <span>状态</span>
            <span>更新时间</span>
            <span>操作</span>
          </div>

          {filtered.length === 0 ? (
            <div className="empty-inline dependency-admin-empty">
              <EmptyState
                title="暂无 MCP 条目。"
                description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可展示数据。"
                action={
                  <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                    新建 MCP
                  </button>
                }
              />
            </div>
          ) : (
            filtered.map((row) => (
              <article className="dependency-admin-table-row" key={row.id}>
                <span className="dependency-admin-cell dependency-admin-cell-name">
                  <strong>{row.name}</strong>
                </span>
                <span className="dependency-admin-cell dependency-admin-cell-address">
                  <code>{row.address}</code>
                </span>
                <span className="dependency-admin-cell">{row.description}</span>
                <span className="dependency-admin-cell">
                  <StateTag tone={row.status === 'active' ? 'running' : 'done'}>
                    {row.status === 'active' ? '启用中' : '已停用'}
                  </StateTag>
                </span>
                <span className="dependency-admin-cell dependency-admin-cell-time">{row.updatedAt}</span>
                <span className="dependency-admin-cell dependency-admin-cell-actions">
                  <button
                    type="button"
                    className="dependency-admin-action"
                    onClick={() => setEditing(row)}
                  >
                    编辑
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
            <select
              className="dependency-admin-page-size"
              aria-label="每页条数"
              value={pageSize}
              onChange={(event) => {
                setPageSize(Number(event.target.value) as PageSize)
                setPage(1)
              }}
            >
              {PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {size} 条/页
                </option>
              ))}
            </select>
          </div>
        </div>
      </section>

      {createOpen ? <McpEditorModal onClose={() => setCreateOpen(false)} /> : null}

      {editing ? <McpEditorModal initial={editing} onClose={() => setEditing(null)} /> : null}

      {pendingDelete ? (
        <DeleteMcpConfirm name={pendingDelete.name} onClose={() => setPendingDelete(null)} />
      ) : null}
    </AppShell>
  )
}
