import { useEffect, useState } from 'react'

import AppShell from '../components/AppShell'
import { EmptyState, Panel, toast } from '../components/ui'
import '../styles/pages-collab.css'

type ProviderKey = 'wecom' | 'feishu' | 'dingtalk'

const PROVIDERS: Array<{ key: ProviderKey; label: string }> = [
  { key: 'wecom', label: '企微' },
  { key: 'feishu', label: '飞书' },
  { key: 'dingtalk', label: '钉钉' },
]

const EMPTY_FORM = {
  name: '',
  provider: 'feishu' as ProviderKey,
  agent: '',
  appId: '',
  appSecret: '',
}

export default function ChannelBots() {
  const [search, setSearch] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [form, setForm] = useState(EMPTY_FORM)
  const [formError, setFormError] = useState('')

  useEffect(() => {
    if (!modalOpen) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setModalOpen(false)
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [modalOpen])

  function openModal() {
    setForm(EMPTY_FORM)
    setFormError('')
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setFormError('')
  }

  function submitForm() {
    if (!form.name.trim()) {
      setFormError('请填写机器人名称')
      return
    }
    if (!form.agent.trim()) {
      setFormError('请填写绑定 Agent')
      return
    }
    setModalOpen(false)
    toast('本地后端尚未提供渠道机器人接口')
  }

  return (
    <AppShell title="渠道机器人列表" breadcrumb="渠道 / 渠道机器人">
      <Panel>
        <div className="toolbar channel-bot-filter-bar">
          <div className="search">
            <input
              type="search"
              value={search}
              placeholder="搜索机器人名称"
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          <button type="button" className="button primary" onClick={openModal}>
            新建机器人
          </button>
        </div>

        {!search.trim() ? (
          <EmptyState
            title="暂无渠道机器人"
            description="本地后端尚未提供该能力。创建机器人后，可在此查看渠道绑定与运行状态。"
            action={
              <button type="button" className="button ghost" onClick={openModal}>
                新建机器人
              </button>
            }
          />
        ) : (
          <div className="table-scroll">
            <div className="data-table channel-bot-table">
              <div className="data-table-row table-head">
                <span>机器人名称</span>
                <span>渠道类型</span>
                <span>绑定 Agent</span>
                <span>状态</span>
                <span>最近活跃</span>
                <span>操作</span>
              </div>
              <div className="data-table-row">
                <span className="cell-muted">暂无符合搜索条件的机器人</span>
                <span>—</span>
                <span>—</span>
                <span>—</span>
                <span>—</span>
                <span className="cell-muted">—</span>
              </div>
            </div>
          </div>
        )}
      </Panel>

      {modalOpen ? (
        <div className="modal-backdrop" onClick={closeModal}>
          <div
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label="新建渠道机器人"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-head">
              <h3>新建渠道机器人</h3>
              <button type="button" className="button ghost small" onClick={closeModal}>
                关闭
              </button>
            </div>
            <div className="modal-body">
              <div className="form-grid">
                <label className="field wide">
                  <span>机器人名称</span>
                  <input
                    className="input"
                    value={form.name}
                    maxLength={120}
                    placeholder="例如：售后助手"
                    onChange={(event) => setForm({ ...form, name: event.target.value })}
                  />
                </label>
                <label className="field">
                  <span>渠道类型</span>
                  <select
                    className="input"
                    value={form.provider}
                    onChange={(event) => setForm({ ...form, provider: event.target.value as ProviderKey })}
                  >
                    {PROVIDERS.map((item) => (
                      <option key={item.key} value={item.key}>
                        {item.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="field">
                  <span>绑定 Agent</span>
                  <input
                    className="input"
                    value={form.agent}
                    placeholder="填写绑定的 Agent 名称"
                    onChange={(event) => setForm({ ...form, agent: event.target.value })}
                  />
                </label>
                <label className="field wide">
                  <span>App ID / Bot ID</span>
                  <input
                    className="input"
                    value={form.appId}
                    onChange={(event) => setForm({ ...form, appId: event.target.value })}
                  />
                </label>
                <label className="field wide">
                  <span>App Secret</span>
                  <input
                    className="input"
                    type="password"
                    value={form.appSecret}
                    onChange={(event) => setForm({ ...form, appSecret: event.target.value })}
                  />
                </label>
              </div>
              {formError ? <div className="notice danger">{formError}</div> : null}
              <div className="notice warn">本地后端尚未提供渠道机器人接口，提交暂不会创建机器人。</div>
            </div>
            <div className="modal-foot">
              <button type="button" className="button" onClick={closeModal}>
                取消
              </button>
              <button type="button" className="button primary" onClick={submitForm}>
                创建
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </AppShell>
  )
}
