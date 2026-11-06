import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, Panel, StateTag, toast } from '../components/ui'
import '../styles/pages-resources-b.css'

/* ------------------------------------------------------------------
   Webhook /webhooks
   对照 hercules-frontend 的 Webhook 管理页：筛选 + 表格 + 新增弹窗。
   本地后端尚未提供该能力，页面不伪造接口与数据。
   ------------------------------------------------------------------ */

interface WebhookRow {
  id: string
  name: string
  url: string
  events: string[]
  enabled: boolean
  lastTriggeredAt: string
}

/** 本地后端尚未提供该能力，接口接入前列表恒为空。 */
const WEBHOOKS: WebhookRow[] = []

const WEBHOOK_EVENT_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'issue.created', label: 'Issue 创建' },
  { value: 'issue.updated', label: 'Issue 更新' },
  { value: 'flow_run.succeeded', label: '流程执行成功' },
  { value: 'flow_run.failed', label: '流程执行失败' },
]

const WEBHOOK_STATUS_OPTIONS: Array<{ value: 'all' | 'enabled' | 'disabled'; label: string }> = [
  { value: 'all', label: '全部状态' },
  { value: 'enabled', label: '启用' },
  { value: 'disabled', label: '停用' },
]

function eventLabel(value: string) {
  return WEBHOOK_EVENT_OPTIONS.find((item) => item.value === value)?.label ?? value
}

function CreateWebhookModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [events, setEvents] = useState<string[]>([])
  const [enabled, setEnabled] = useState(true)
  const [notice, setNotice] = useState('')

  const toggleEvent = (value: string) => {
    setEvents((current) =>
      current.includes(value) ? current.filter((item) => item !== value) : [...current, value],
    )
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) {
      setNotice('请输入 Webhook 名称')
      return
    }
    if (!url.trim()) {
      setNotice('请输入 Webhook 地址')
      return
    }
    if (!/^https?:\/\//i.test(url.trim())) {
      setNotice('Webhook 地址必须以 http:// 或 https:// 开头')
      return
    }
    if (!events.length) {
      setNotice('请至少选择一个事件类型')
      return
    }
    setNotice('本地后端尚未提供该能力，创建 Webhook 不会写入数据库。')
    toast(`本地后端尚未提供该能力，未能创建 Webhook：${name.trim()}。`)
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="新增 Webhook" style={{ width: 'min(680px, 100%)' }}>
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>新增 Webhook</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="rb-form">
              <label className="field wide">
                <span>
                  名称 <em className="rb-required">*</em>
                </span>
                <input
                  value={name}
                  maxLength={255}
                  placeholder="例如：线上日志告警分析"
                  onChange={(event) => setName(event.target.value)}
                />
              </label>

              <label className="field wide">
                <span>
                  Webhook 地址 <em className="rb-required">*</em>
                </span>
                <input
                  value={url}
                  maxLength={2048}
                  placeholder="https://example.com/hooks/aladdin"
                  onChange={(event) => setUrl(event.target.value)}
                />
              </label>

              <div className="field wide">
                <span>
                  事件类型 <em className="rb-required">*</em>
                </span>
                <div className="rb-checks">
                  {WEBHOOK_EVENT_OPTIONS.map((item) => (
                    <label key={item.value} className="rb-check">
                      <input
                        type="checkbox"
                        checked={events.includes(item.value)}
                        onChange={() => toggleEvent(item.value)}
                      />
                      {item.label}
                    </label>
                  ))}
                </div>
              </div>

              <div className="field wide">
                <span>状态</span>
                <label className="rb-switch">
                  <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
                  <span className="rb-switch-track" aria-hidden />
                  {enabled ? '启用' : '停用'}
                </label>
              </div>

              <p className="rb-form-note wide">
                触发事件时平台会向该地址发送 POST 请求。本地后端尚未提供该能力，创建操作暂不会写入数据库。
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

export default function Webhooks() {
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState<'all' | 'enabled' | 'disabled'>('all')
  const [createOpen, setCreateOpen] = useState(false)

  const rows = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    return WEBHOOKS.filter((row) => {
      if (status === 'enabled' && !row.enabled) return false
      if (status === 'disabled' && row.enabled) return false
      if (!query) return true
      return row.name.toLowerCase().includes(query)
    })
  }, [keyword, status])

  return (
    <AppShell title="Webhook" breadcrumb="资源管理 / Webhook">
      <Panel title="Webhook 列表" subtitle="配置事件回调地址，在 Issue 与流程运行时接收通知。">
        <div className="rb-toolbar">
          <div className="rb-toolbar-side">
            <div className="rb-search">
              <IconSearch width={15} height={15} />
              <input
                value={keyword}
                aria-label="搜索 Webhook"
                placeholder="搜索 Webhook 名称"
                onChange={(event) => setKeyword(event.target.value)}
              />
            </div>
            <select
              className="rb-select"
              aria-label="按状态筛选"
              value={status}
              onChange={(event) => setStatus(event.target.value as 'all' | 'enabled' | 'disabled')}
            >
              {WEBHOOK_STATUS_OPTIONS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </div>
          <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
            <IconPlus width={15} height={15} /> 新增 Webhook
          </button>
        </div>

        {rows.length ? (
          <div className="rb-table cols-6">
            <div className="rb-table-head">
              <span>名称</span>
              <span>URL</span>
              <span>事件类型</span>
              <span>状态</span>
              <span>最近触发</span>
              <span>操作</span>
            </div>
            {rows.map((row) => (
              <div className="rb-row" key={row.id}>
                <div className="rb-cell rb-cell-name">
                  <strong title={row.name}>{row.name}</strong>
                </div>
                <div className="rb-cell">
                  <span className="rb-url" title={row.url}>
                    {row.url || '-'}
                  </span>
                </div>
                <div className="rb-cell">
                  {row.events.length ? (
                    <div className="rb-events">
                      {row.events.map((value) => (
                        <span key={value} className="rb-event">
                          {eventLabel(value)}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <span className="rb-muted">-</span>
                  )}
                </div>
                <div className="rb-cell">
                  <StateTag tone={row.enabled ? 'done' : 'muted'}>{row.enabled ? '启用' : '停用'}</StateTag>
                </div>
                <div className="rb-cell rb-muted">{row.lastTriggeredAt || '暂无触发'}</div>
                <div className="rb-cell">
                  <div className="rb-actions">
                    <button
                      type="button"
                      className="rb-action"
                      onClick={() => toast('本地后端尚未提供该能力，无法编辑 Webhook。')}
                    >
                      编辑
                    </button>
                    <span className="rb-divider">|</span>
                    <button
                      type="button"
                      className="rb-action danger"
                      onClick={() => toast('本地后端尚未提供该能力，无法删除 Webhook。')}
                    >
                      删除
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="rb-empty">
            <EmptyState
              title="暂无 Webhook"
              description="本地后端尚未提供该能力。页面结构按阿拉丁设计还原，接入接口后即可展示事件回调配置。"
              action={
                <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                  新增 Webhook
                </button>
              }
            />
          </div>
        )}
      </Panel>

      {createOpen ? <CreateWebhookModal onClose={() => setCreateOpen(false)} /> : null}
    </AppShell>
  )
}
