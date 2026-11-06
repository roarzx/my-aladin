import { useEffect, useState } from 'react'

import AppShell from '../components/AppShell'
import { Panel, toast } from '../components/ui'
import '../styles/pages-collab.css'

type StatusFilter = 'all' | 'enabled' | 'paused'
type TriggerType = 'cron' | 'once'

const STATUS_FILTERS: Array<{ key: StatusFilter; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'enabled', label: '启用中' },
  { key: 'paused', label: '已停用' },
]

const EMPTY_FORM = {
  name: '',
  triggerType: 'cron' as TriggerType,
  cronExpr: '',
  runAt: '',
  agent: '',
  instruction: '',
}

export default function TaskSchedules() {
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
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
      setFormError('请填写任务名称')
      return
    }
    if (form.triggerType === 'cron' && !form.cronExpr.trim()) {
      setFormError('请填写 Cron 表达式')
      return
    }
    if (form.triggerType === 'once' && !form.runAt) {
      setFormError('请选择执行时间')
      return
    }
    if (!form.agent.trim()) {
      setFormError('请填写目标 Agent')
      return
    }
    if (!form.instruction.trim()) {
      setFormError('请填写指令内容')
      return
    }
    setModalOpen(false)
    toast('本地后端尚未提供自动化任务接口')
  }

  return (
    <AppShell title="自动化任务" breadcrumb="自动化任务">
      <Panel
        title="自动化任务"
        subtitle="管理当前工作区下定时执行的 Agent 自动化任务。"
        actions={
          <button type="button" className="button primary" onClick={openModal}>
            新建任务
          </button>
        }
      >
        <div className="toolbar task-schedule-toolbar">
          <div className="filters">
            {STATUS_FILTERS.map((item) => (
              <button
                key={item.key}
                type="button"
                className={`filter${statusFilter === item.key ? ' active' : ''}`}
                onClick={() => setStatusFilter(item.key)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <div className="search">
            <input
              type="search"
              value={search}
              placeholder="搜索任务名称"
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
        </div>

        <div className="table-scroll">
          <div className="data-table task-schedule-table">
            <div className="data-table-row table-head">
              <span>名称</span>
              <span>Agent</span>
              <span>执行计划</span>
              <span>状态</span>
              <span>创建人</span>
              <span>修改人</span>
              <span>创建时间</span>
              <span>修改时间</span>
              <span>操作</span>
            </div>
            <div className="data-table-row">
              <span className="cell-muted" style={{ gridColumn: '1 / -1' }}>
                {statusFilter === 'all' && !search.trim()
                  ? '暂无自动化任务（本地后端尚未提供该能力）'
                  : '暂无符合筛选条件的任务'}
              </span>
            </div>
          </div>
        </div>
      </Panel>

      {modalOpen ? (
        <div className="modal-backdrop" onClick={closeModal}>
          <div
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label="新建自动化任务"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-head">
              <h3>新建自动化任务</h3>
              <button type="button" className="button ghost small" onClick={closeModal}>
                关闭
              </button>
            </div>
            <div className="modal-body">
              <div className="form-grid">
                <label className="field wide">
                  <span>任务名称</span>
                  <input
                    className="input"
                    value={form.name}
                    maxLength={120}
                    placeholder="例如：每日工单巡检"
                    onChange={(event) => setForm({ ...form, name: event.target.value })}
                  />
                </label>
                <label className="field">
                  <span>触发方式</span>
                  <select
                    className="input"
                    value={form.triggerType}
                    onChange={(event) => setForm({ ...form, triggerType: event.target.value as TriggerType })}
                  >
                    <option value="cron">定时 Cron</option>
                    <option value="once">一次性</option>
                  </select>
                </label>
                {form.triggerType === 'cron' ? (
                  <label className="field">
                    <span>Cron 表达式</span>
                    <input
                      className="input"
                      value={form.cronExpr}
                      placeholder="0 9 * * 1-5"
                      onChange={(event) => setForm({ ...form, cronExpr: event.target.value })}
                    />
                  </label>
                ) : (
                  <label className="field">
                    <span>执行时间</span>
                    <input
                      className="input"
                      type="datetime-local"
                      value={form.runAt}
                      onChange={(event) => setForm({ ...form, runAt: event.target.value })}
                    />
                  </label>
                )}
                <label className="field wide">
                  <span>目标 Agent</span>
                  <input
                    className="input"
                    value={form.agent}
                    placeholder="填写目标 Agent 名称"
                    onChange={(event) => setForm({ ...form, agent: event.target.value })}
                  />
                </label>
                <label className="field wide">
                  <span>指令内容</span>
                  <textarea
                    className="input"
                    rows={6}
                    value={form.instruction}
                    placeholder="定时任务触发后发送给 Agent 的指令"
                    onChange={(event) => setForm({ ...form, instruction: event.target.value })}
                  />
                </label>
              </div>
              {formError ? <div className="notice danger">{formError}</div> : null}
              <div className="notice warn">本地后端尚未提供自动化任务接口，提交暂不会创建任务。</div>
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
