import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, StateTag } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   知识库管理
   对照 hercules-frontend KnowledgeBasesPage。
   本地 Go 后端未提供知识库接口，列表恒为空，不伪造数据。
   ------------------------------------------------------------------ */

const STATUS_FILTERS = [
  { label: '全部', value: 'all' },
  { label: '启用中', value: 'active' },
  { label: '已停用', value: 'inactive' },
] as const

type StatusFilter = (typeof STATUS_FILTERS)[number]['value']

const PAGE_SIZES = [10, 20, 50] as const
type PageSize = (typeof PAGE_SIZES)[number]

type SourceKind = 'existing' | 'generate'

interface KnowledgeBaseRow {
  id: string
  name: string
  source: string
  address: string
  generation: string
  status: 'active' | 'inactive'
  updatedAt: string
}

/** 本地后端尚未提供知识库能力，接口接入前列表恒为空。 */
const ROWS: KnowledgeBaseRow[] = []

const SOURCE_OPTIONS: Array<{ value: SourceKind; title: string; description: string }> = [
  {
    value: 'existing',
    title: '我已有知识库仓库',
    description: '直接绑定一个已存在的知识库仓库，保存后即可被任务和会话引用。',
  },
  {
    value: 'generate',
    title: '从代码仓库生成',
    description: '指定代码仓库与生成提示词，由系统自动生成可直接使用的知识库。',
  },
]

interface CreateDraft {
  name: string
  source: SourceKind
  remoteAddress: string
  targetRepo: string
  repoCode: string
  usageRule: string
  prompt: string
  status: 'active' | 'inactive'
}

const EMPTY_DRAFT: CreateDraft = {
  name: '',
  source: 'existing',
  remoteAddress: '',
  targetRepo: '',
  repoCode: '',
  usageRule: '',
  prompt: '',
  status: 'active',
}

function CreateKnowledgeBaseModal({ onClose }: { onClose: () => void }) {
  const [draft, setDraft] = useState<CreateDraft>(EMPTY_DRAFT)
  const [notice, setNotice] = useState('')

  const patch = (next: Partial<CreateDraft>) => setDraft((prev) => ({ ...prev, ...next }))

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!draft.name.trim()) {
      setNotice('请填写知识库名称。')
      return
    }
    if (draft.source === 'existing' && !draft.remoteAddress.trim()) {
      setNotice('请填写远程仓库地址。')
      return
    }
    if (draft.source === 'generate' && (!draft.targetRepo.trim() || !draft.repoCode.trim())) {
      setNotice('请填写存放知识库仓库与仓库代码。')
      return
    }
    setNotice('本地后端尚未提供该能力，创建不会写入数据库。')
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="新建知识库">
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>新建知识库</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="dependency-form-grid">
              <label className="field wide">
                <span>知识库名称</span>
                <input
                  value={draft.name}
                  placeholder="请输入知识库名称"
                  onChange={(event) => patch({ name: event.target.value })}
                />
              </label>

              <div className="field wide">
                <span>知识库来源</span>
                <div className="dependency-source-cards">
                  {SOURCE_OPTIONS.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      className={`dependency-source-card ${draft.source === option.value ? 'selected' : ''}`}
                      aria-pressed={draft.source === option.value}
                      onClick={() => patch({ source: option.value })}
                    >
                      <strong>{option.title}</strong>
                      <span>{option.description}</span>
                    </button>
                  ))}
                </div>
              </div>

              {draft.source === 'existing' ? (
                <label className="field wide">
                  <span>远程仓库地址</span>
                  <input
                    value={draft.remoteAddress}
                    placeholder="请填入知识库仓库地址"
                    onChange={(event) => patch({ remoteAddress: event.target.value })}
                  />
                </label>
              ) : (
                <>
                  <label className="field wide">
                    <span>存放知识库仓库</span>
                    <input
                      value={draft.targetRepo}
                      placeholder="请填入用于存放知识库的仓库地址"
                      onChange={(event) => patch({ targetRepo: event.target.value })}
                    />
                  </label>
                  <label className="field wide">
                    <span>仓库代码</span>
                    <input
                      value={draft.repoCode}
                      placeholder="请填入需要生成知识库的代码仓库"
                      onChange={(event) => patch({ repoCode: event.target.value })}
                    />
                  </label>
                  <label className="field wide">
                    <span>生成提示词</span>
                    <textarea
                      rows={4}
                      value={draft.prompt}
                      placeholder="描述希望生成的知识库内容与组织方式"
                      onChange={(event) => patch({ prompt: event.target.value })}
                    />
                  </label>
                </>
              )}

              <label className="field wide">
                <span>知识库使用规则</span>
                <textarea
                  rows={4}
                  value={draft.usageRule}
                  placeholder="描述 Agent 何时应该使用该知识库"
                  onChange={(event) => patch({ usageRule: event.target.value })}
                />
              </label>

              <label className="field wide">
                <span>状态</span>
                <select
                  value={draft.status}
                  onChange={(event) => patch({ status: event.target.value as CreateDraft['status'] })}
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
              创建
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function DeleteKnowledgeBaseConfirm({ name, onClose }: { name: string; onClose: () => void }) {
  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="删除知识库">
        <div className="modal-head">
          <h3>删除知识库</h3>
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

export default function KnowledgeBases() {
  const [status, setStatus] = useState<StatusFilter>('all')
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(10)
  const [createOpen, setCreateOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<KnowledgeBaseRow | null>(null)

  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase()
    return ROWS.filter((row) => {
      if (status !== 'all' && row.status !== status) return false
      if (!needle) return true
      return (
        row.name.toLowerCase().includes(needle) ||
        row.address.toLowerCase().includes(needle) ||
        row.source.toLowerCase().includes(needle)
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
    <AppShell title="知识库管理" breadcrumb="依赖管理 / 知识库管理">
      <section className="panel dependency-admin-panel">
        <div className="panel-head">
          <div>
            <h2>知识库管理</h2>
            <p>管理任务和会话可引用的知识库，支持已有仓库和代码仓库生成。</p>
          </div>
          <div className="panel-actions dependency-admin-action-group">
            <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
              <IconPlus width={15} height={15} /> 新建知识库
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
              placeholder="搜索知识库名称、地址、描述或来源"
              aria-label="搜索知识库名称、地址、描述或来源"
              onChange={(event) => setSearchInput(event.target.value)}
            />
          </form>
        </div>

        <div className="dependency-admin-table cols-7">
          <div className="dependency-admin-table-head">
            <span>名称</span>
            <span>来源</span>
            <span>地址 / 来源摘要</span>
            <span>生成状态</span>
            <span>状态</span>
            <span>更新时间</span>
            <span>操作</span>
          </div>

          {filtered.length === 0 ? (
            <div className="empty-inline dependency-admin-empty">
              <EmptyState
                title="暂无知识库条目。"
                description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可展示数据。"
                action={
                  <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                    新建知识库
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
                <span className="dependency-admin-cell">
                  <span className="knowledge-source-tag">{row.source}</span>
                </span>
                <span className="dependency-admin-cell dependency-admin-cell-address">
                  <code>{row.address}</code>
                </span>
                <span className="dependency-admin-cell">
                  <span className="knowledge-generation">{row.generation}</span>
                </span>
                <span className="dependency-admin-cell">
                  <StateTag tone={row.status === 'active' ? 'running' : 'done'}>
                    {row.status === 'active' ? '启用中' : '已停用'}
                  </StateTag>
                </span>
                <span className="dependency-admin-cell dependency-admin-cell-time">{row.updatedAt}</span>
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

      {createOpen ? <CreateKnowledgeBaseModal onClose={() => setCreateOpen(false)} /> : null}

      {pendingDelete ? (
        <DeleteKnowledgeBaseConfirm
          name={pendingDelete.name}
          onClose={() => setPendingDelete(null)}
        />
      ) : null}
    </AppShell>
  )
}
