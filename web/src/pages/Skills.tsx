import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, StateTag } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   依赖管理（Skills / MCP）统一后台骨架
   对照 hercules-frontend DependencyResourcePage。
   本地 Go 后端仅提供 Issue 接口，这里不伪造数据。
   ------------------------------------------------------------------ */

const STATUS_FILTERS = [
  { label: '全部', value: 'all' },
  { label: '启用中', value: 'active' },
  { label: '已停用', value: 'inactive' },
] as const

type StatusFilter = (typeof STATUS_FILTERS)[number]['value']

const PAGE_SIZES = [10, 20, 50] as const

type PageSize = (typeof PAGE_SIZES)[number]

interface ResourceRow {
  id: string
  name: string
  address: string
  description: string
  status: 'active' | 'inactive'
  createdAt: string
  updatedAt: string
  createdBy: string
  updatedBy: string
}

/** 本地后端尚未提供依赖管理能力，接口接入前列表恒为空。 */
const ROWS: ResourceRow[] = []

interface EditorDraft {
  name: string
  address: string
  description: string
  authType: 'custom' | 'domain'
  authSecret: string
  status: 'active' | 'inactive'
}

const EMPTY_DRAFT: EditorDraft = {
  name: '',
  address: '',
  description: '',
  authType: 'custom',
  authSecret: '',
  status: 'active',
}

export interface DependencyResourcePageProps {
  title: string
  breadcrumb: string
  subtitle: string
  emptyText: string
  createLabel: string
  searchPlaceholder: string
  addressLabel: string
  addressPlaceholder: string
  /** MCP 条目需要额外的认证方式配置。 */
  withAuth?: boolean
  /** Skills 头部提供「批量导入」入口。 */
  batchImport?: boolean
}

function EditorModal({
  title,
  okText,
  addressLabel,
  addressPlaceholder,
  withAuth,
  onClose,
}: {
  title: string
  okText: string
  addressLabel: string
  addressPlaceholder: string
  withAuth?: boolean
  onClose: () => void
}) {
  const [draft, setDraft] = useState<EditorDraft>(EMPTY_DRAFT)
  const [notice, setNotice] = useState('')

  const patch = (next: Partial<EditorDraft>) => setDraft((prev) => ({ ...prev, ...next }))

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!draft.name.trim()) {
      setNotice('请填写名称后再保存。')
      return
    }
    if (withAuth && draft.authType === 'custom' && !draft.authSecret.trim()) {
      setNotice('请填写认证信息后再保存。')
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
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>{title}</h3>
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
                  placeholder="请输入名称"
                  onChange={(event) => patch({ name: event.target.value })}
                />
              </label>

              <label className="field wide">
                <span>{addressLabel}</span>
                <input
                  value={draft.address}
                  placeholder={addressPlaceholder}
                  onChange={(event) => patch({ address: event.target.value })}
                />
              </label>

              <label className="field wide">
                <span>描述</span>
                <textarea
                  rows={4}
                  value={draft.description}
                  placeholder="请填写描述"
                  onChange={(event) => patch({ description: event.target.value })}
                />
              </label>

              {withAuth ? (
                <>
                  <div className="field wide">
                    <span id="dependency-auth-type-label">认证方式</span>
                    <div className="dependency-radio-row" role="radiogroup" aria-labelledby="dependency-auth-type-label">
                      <label>
                        <input
                          type="radio"
                          name="dependency-auth-type"
                          checked={draft.authType === 'custom'}
                          onChange={() => patch({ authType: 'custom' })}
                        />
                        自定义
                      </label>
                      <label>
                        <input
                          type="radio"
                          name="dependency-auth-type"
                          checked={draft.authType === 'domain'}
                          onChange={() => patch({ authType: 'domain' })}
                        />
                        域账号认证
                      </label>
                    </div>
                  </div>

                  {draft.authType === 'domain' ? (
                    <div className="field wide">
                      <span>使用实际调用者的域账号</span>
                    </div>
                  ) : (
                    <label className="field wide">
                      <span>认证信息</span>
                      <input
                        value={draft.authSecret}
                        placeholder="示例: Bearer xxxxxxxxxx"
                        onChange={(event) => patch({ authSecret: event.target.value })}
                      />
                    </label>
                  )}
                </>
              ) : null}

              <label className="field wide">
                <span>状态</span>
                <select
                  value={draft.status}
                  onChange={(event) => patch({ status: event.target.value as EditorDraft['status'] })}
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
              {okText}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function DeleteConfirm({ name, onClose }: { name: string; onClose: () => void }) {
  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="删除">
        <div className="modal-head">
          <h3>删除</h3>
        </div>
        <div className="modal-body">
          <p>确认删除「{name}」？</p>
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

export function DependencyResourcePage({
  title,
  breadcrumb,
  subtitle,
  emptyText,
  createLabel,
  searchPlaceholder,
  addressLabel,
  addressPlaceholder,
  withAuth,
  batchImport,
}: DependencyResourcePageProps) {
  const [status, setStatus] = useState<StatusFilter>('all')
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(10)
  const [editorOpen, setEditorOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ResourceRow | null>(null)
  const [batchNotice, setBatchNotice] = useState('')

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
    <AppShell title={title} breadcrumb={breadcrumb}>
      <section className="panel dependency-admin-panel">
        <div className="panel-head">
          <div>
            <h2>{title}</h2>
            <p>{subtitle}</p>
          </div>
          <div className="panel-actions dependency-admin-action-group">
            {batchImport ? (
              <button
                type="button"
                className="button ghost"
                onClick={() => setBatchNotice('本地后端尚未提供该能力，批量导入暂不可用。')}
              >
                批量导入
              </button>
            ) : null}
            <button type="button" className="button primary" onClick={() => setEditorOpen(true)}>
              <IconPlus width={15} height={15} /> {createLabel}
            </button>
          </div>
        </div>

        {batchNotice ? (
          <p className="notice warn" role="status" style={{ marginBottom: 12 }}>
            {batchNotice}
          </p>
        ) : null}

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
              placeholder={searchPlaceholder}
              aria-label={searchPlaceholder}
              onChange={(event) => setSearchInput(event.target.value)}
            />
          </form>
        </div>

        <div className="dependency-admin-table cols-9">
          <div className="dependency-admin-table-head">
            <span>名称</span>
            <span>{addressLabel}</span>
            <span>描述</span>
            <span>状态</span>
            <span>创建时间</span>
            <span>更新时间</span>
            <span>创建人</span>
            <span>更新人</span>
            <span>操作</span>
          </div>

          {filtered.length === 0 ? (
            <div className="empty-inline dependency-admin-empty">
              <EmptyState
                title={emptyText}
                description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可展示数据。"
                action={
                  <button type="button" className="button ghost" onClick={() => setEditorOpen(true)}>
                    {createLabel}
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
                <span className="dependency-admin-cell dependency-admin-cell-remark">{row.description}</span>
                <span className="dependency-admin-cell">
                  <StateTag tone={row.status === 'active' ? 'running' : 'done'}>
                    {row.status === 'active' ? '启用中' : '已停用'}
                  </StateTag>
                </span>
                <span className="dependency-admin-cell dependency-admin-cell-time">{row.createdAt}</span>
                <span className="dependency-admin-cell dependency-admin-cell-time">{row.updatedAt}</span>
                <span className="dependency-admin-cell">{row.createdBy}</span>
                <span className="dependency-admin-cell">{row.updatedBy}</span>
                <span className="dependency-admin-cell dependency-admin-cell-actions">
                  <button type="button" className="dependency-admin-action">
                    编辑
                  </button>
                  <span className="dependency-admin-action-divider">|</span>
                  <button type="button" className="dependency-admin-action">
                    {row.status === 'active' ? '停用' : '启用'}
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

      {editorOpen ? (
        <EditorModal
          title={createLabel}
          okText="创建"
          addressLabel={addressLabel}
          addressPlaceholder={addressPlaceholder}
          withAuth={withAuth}
          onClose={() => setEditorOpen(false)}
        />
      ) : null}

      {pendingDelete ? (
        <DeleteConfirm name={pendingDelete.name} onClose={() => setPendingDelete(null)} />
      ) : null}
    </AppShell>
  )
}

export default function Skills() {
  return (
    <DependencyResourcePage
      title="Skills 管理"
      breadcrumb="依赖管理 / Skills 管理"
      subtitle="管理可供 Agent 引用的 Skills。"
      emptyText="暂无 Skill 条目。"
      createLabel="新建 Skill"
      searchPlaceholder="搜索 Skills 名称、地址或描述"
      addressLabel="地址"
      addressPlaceholder="请填入 Git 地址"
      batchImport
    />
  )
}
