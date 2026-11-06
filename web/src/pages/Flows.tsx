import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, Panel, StateTag, toast } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-resources-b.css'

/* ------------------------------------------------------------------
   流程管理 /resources/flows
   对照 hercules-frontend 的流程管理页：工具栏 + 流程表格 + 新建流程弹窗。
   本地后端尚未提供该能力，页面不伪造接口与数据。
   ------------------------------------------------------------------ */

type FlowStatus = 'enabled' | 'disabled'

interface FlowRow {
  id: string
  name: string
  description: string
  version: string
  nodeCount: number
  status: FlowStatus
  updatedAt: string
}

/** 本地后端尚未提供该能力，接口接入前列表恒为空。 */
const FLOWS: FlowRow[] = []

const FLOW_STATUS: Record<FlowStatus, { label: string; tone: string }> = {
  enabled: { label: '启用中', tone: 'done' },
  disabled: { label: '已停用', tone: 'muted' },
}

const FLOW_FILTERS: Array<{ value: 'all' | FlowStatus; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'enabled', label: '启用中' },
  { value: 'disabled', label: '已停用' },
]

function CreateFlowModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [type, setType] = useState('acceptance_test')
  const [status, setStatus] = useState<FlowStatus>('enabled')
  const [description, setDescription] = useState('')
  const [notice, setNotice] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) {
      setNotice('流程名称不能为空')
      return
    }
    if (name.trim().length > 255) {
      setNotice('流程名称不能超过255个字符。')
      return
    }
    setNotice('本地后端尚未提供该能力，保存流程不会写入数据库。')
    toast(`本地后端尚未提供该能力，未能保存流程：${name.trim()}。`)
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="新建流程" style={{ width: 'min(760px, 100%)' }}>
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>新建流程</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <p className="rb-form-note" style={{ marginBottom: 12 }}>
              基础信息
              <br />
              定义流程在列表、运行选择和执行详情中的展示信息。
            </p>
            <div className="rb-form">
              <label className="field wide">
                <span>
                  流程名称 <em className="rb-required">*</em>
                </span>
                <input
                  value={name}
                  maxLength={255}
                  placeholder="请输入流程名称"
                  onChange={(event) => setName(event.target.value)}
                />
              </label>

              <label className="field">
                <span>流程类型</span>
                <select value={type} onChange={(event) => setType(event.target.value)}>
                  <option value="acceptance_test">接测</option>
                </select>
              </label>

              <label className="field">
                <span>状态</span>
                <select value={status} onChange={(event) => setStatus(event.target.value as FlowStatus)}>
                  <option value="enabled">启用中</option>
                  <option value="disabled">已停用</option>
                </select>
              </label>

              <label className="field wide">
                <span>流程说明</span>
                <textarea
                  value={description}
                  maxLength={500}
                  placeholder="说明流程适用场景、执行步骤与注意事项"
                  onChange={(event) => setDescription(event.target.value)}
                />
              </label>

              <p className="rb-form-note wide">
                流程保存后可在 Issue 会话中直接运行。本地后端尚未提供该能力，创建操作暂不会写入数据库。
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
              保存并发布
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default function Flows() {
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState<'all' | FlowStatus>('all')
  const [createOpen, setCreateOpen] = useState(false)

  const rows = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    return FLOWS.filter((row) => {
      if (status !== 'all' && row.status !== status) return false
      if (!query) return true
      return [row.name, row.description].some((value) => value.toLowerCase().includes(query))
    })
  }, [keyword, status])

  return (
    <AppShell title="流程管理" breadcrumb="资源管理 / 流程管理">
      <Panel title="流程管理" subtitle="维护 Issue 可直接运行的流程。运行时由当前会话指定 Agent。">
        <div className="rb-toolbar">
          <div className="rb-toolbar-side">
            <div className="rb-search">
              <IconSearch width={15} height={15} />
              <input
                value={keyword}
                aria-label="搜索流程"
                placeholder="搜索流程名称、说明或节点名称"
                onChange={(event) => setKeyword(event.target.value)}
              />
            </div>
            <select
              className="rb-select"
              aria-label="按状态筛选"
              value={status}
              onChange={(event) => setStatus(event.target.value as 'all' | FlowStatus)}
            >
              {FLOW_FILTERS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </div>
          <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
            <IconPlus width={15} height={15} /> 新建流程
          </button>
        </div>

        {rows.length ? (
          <div className="rb-table cols-6 flows">
            <div className="rb-table-head">
              <span>流程名称</span>
              <span>版本</span>
              <span>节点数</span>
              <span>状态</span>
              <span>更新时间</span>
              <span>操作</span>
            </div>
            {rows.map((row) => {
              const meta = FLOW_STATUS[row.status]
              return (
                <div className="rb-row" key={row.id}>
                  <div className="rb-cell rb-cell-name">
                    <strong title={row.name}>{row.name}</strong>
                  </div>
                  <div className="rb-cell rb-muted">{row.version ? `v${row.version}` : '-'}</div>
                  <div className="rb-cell rb-muted">{row.nodeCount} 个</div>
                  <div className="rb-cell">
                    <StateTag tone={meta.tone}>{meta.label}</StateTag>
                  </div>
                  <div className="rb-cell rb-muted">{row.updatedAt || '-'}</div>
                  <div className="rb-cell">
                    <div className="rb-actions">
                      <Link className="rb-action" to={`/resources/flow-runs?id=${encodeURIComponent(row.id)}`}>
                        查看执行记录
                      </Link>
                      <span className="rb-divider">|</span>
                      <button
                        type="button"
                        className="rb-action"
                        onClick={() => toast('本地后端尚未提供该能力，无法编辑流程。')}
                      >
                        编辑
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
              title="暂无流程"
              description="本地后端尚未提供该能力。页面结构按阿拉丁设计还原，接入接口后即可展示 Issue 可直接运行的流程。"
              action={
                <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                  新建流程
                </button>
              }
            />
          </div>
        )}
      </Panel>

      {createOpen ? <CreateFlowModal onClose={() => setCreateOpen(false)} /> : null}
    </AppShell>
  )
}
