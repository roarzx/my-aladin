import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, Panel, StateTag, toast } from '../components/ui'
import '../styles/pages-resources-b.css'

/* ------------------------------------------------------------------
   项目管理 /resources/templates
   对照 hercules-frontend 的项目管理页：工具栏 + 数据表格 + 新建项目弹窗。
   本地后端尚未提供该能力，页面不伪造接口与数据。
   ------------------------------------------------------------------ */

type ProjectStatus = 'active' | 'disabled'
type ProjectType = 'standard' | 'acceptance_test'

interface ProjectRow {
  id: string
  name: string
  description: string
  type: ProjectType
  status: ProjectStatus
  updatedAt: string
  updatedBy: string
}

/** 本地后端尚未提供该能力，接口接入前列表恒为空。 */
const PROJECTS: ProjectRow[] = []

const PROJECT_TYPES: Array<{ value: ProjectType; label: string }> = [
  { value: 'standard', label: '通用项目' },
  { value: 'acceptance_test', label: '接测项目' },
]

const PROJECT_STATUS: Record<ProjectStatus, { label: string; tone: string }> = {
  active: { label: '启用中', tone: 'done' },
  disabled: { label: '已停用', tone: 'muted' },
}

const PROJECT_STATUS_OPTIONS: Array<{ value: 'all' | ProjectStatus; label: string }> = [
  { value: 'all', label: '全部状态' },
  { value: 'active', label: '启用中' },
  { value: 'disabled', label: '已停用' },
]

function CreateProjectModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [type, setType] = useState<ProjectType>('standard')
  const [description, setDescription] = useState('')
  const [notice, setNotice] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) {
      setNotice('项目名称不能为空')
      return
    }
    setNotice('本地后端尚未提供该能力，创建项目不会写入数据库。')
    toast(`本地后端尚未提供该能力，未能创建项目：${name.trim()}。`)
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="新建项目" style={{ width: 'min(680px, 100%)' }}>
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>新建项目</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="rb-form">
              <label className="field wide">
                <span>
                  项目名称 <em className="rb-required">*</em>
                </span>
                <input
                  value={name}
                  maxLength={100}
                  placeholder="请输入项目名称"
                  onChange={(event) => setName(event.target.value)}
                />
              </label>

              <label className="field">
                <span>类型</span>
                <select value={type} onChange={(event) => setType(event.target.value as ProjectType)}>
                  {PROJECT_TYPES.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </select>
              </label>

              <label className="field wide">
                <span>描述</span>
                <textarea
                  value={description}
                  maxLength={500}
                  placeholder="补充项目用途、依赖说明等"
                  onChange={(event) => setDescription(event.target.value)}
                />
              </label>

              <p className="rb-form-note wide">
                项目用于创建 Issue 时复用配置。本地后端尚未提供该能力，创建操作暂不会写入数据库。
              </p>

              {notice ? (
                <p className="notice warn wide" role="status">
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

export default function Templates() {
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState<'all' | ProjectStatus>('all')
  const [createOpen, setCreateOpen] = useState(false)

  const rows = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    return PROJECTS.filter((row) => {
      if (status !== 'all' && row.status !== status) return false
      if (!query) return true
      return [row.name, row.description, row.updatedBy].some((value) =>
        value.toLowerCase().includes(query),
      )
    })
  }, [keyword, status])

  return (
    <AppShell title="项目管理" breadcrumb="资源管理 / 项目管理">
      <Panel title="项目管理" subtitle="统一管理创建 Issue 时可复用的项目配置。">
        <div className="rb-toolbar">
          <div className="rb-toolbar-side">
            <div className="rb-search">
              <IconSearch width={15} height={15} />
              <input
                value={keyword}
                aria-label="搜索项目"
                placeholder="搜索项目名称、备注或依赖摘要"
                onChange={(event) => setKeyword(event.target.value)}
              />
            </div>
            <select
              className="rb-select"
              aria-label="按状态筛选"
              value={status}
              onChange={(event) => setStatus(event.target.value as 'all' | ProjectStatus)}
            >
              {PROJECT_STATUS_OPTIONS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </div>
          <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
            <IconPlus width={15} height={15} /> 新建项目
          </button>
        </div>

        {rows.length ? (
          <div className="rb-table cols-5">
            <div className="rb-table-head">
              <span>项目名称</span>
              <span>类型</span>
              <span>状态</span>
              <span>最近更新</span>
              <span>操作</span>
            </div>
            {rows.map((row) => {
              const meta = PROJECT_STATUS[row.status]
              return (
                <div className="rb-row" key={row.id}>
                  <div className="rb-cell rb-cell-name">
                    <strong title={row.name}>{row.name}</strong>
                  </div>
                  <div className="rb-cell rb-muted">
                    {PROJECT_TYPES.find((item) => item.value === row.type)?.label ?? row.type}
                  </div>
                  <div className="rb-cell">
                    <StateTag tone={meta.tone}>{meta.label}</StateTag>
                  </div>
                  <div className="rb-cell rb-muted" title={`${row.updatedAt} ${row.updatedBy}`}>
                    {row.updatedAt || '-'}
                  </div>
                  <div className="rb-cell">
                    <div className="rb-actions">
                      <button
                        type="button"
                        className="rb-action"
                        onClick={() => toast('本地后端尚未提供该能力，无法编辑项目。')}
                      >
                        编辑
                      </button>
                      <span className="rb-divider">|</span>
                      <button
                        type="button"
                        className="rb-action"
                        onClick={() => toast('本地后端尚未提供该能力，无法复制项目。')}
                      >
                        复制
                      </button>
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        ) : (
          <div className="rb-empty">
            <EmptyState
              title="暂无项目"
              description="本地后端尚未提供该能力。页面结构按阿拉丁设计还原，接入接口后即可展示项目配置。"
              action={
                <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                  新建项目
                </button>
              }
            />
          </div>
        )}
      </Panel>

      {createOpen ? <CreateProjectModal onClose={() => setCreateOpen(false)} /> : null}
    </AppShell>
  )
}
