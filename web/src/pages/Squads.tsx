import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconRefresh, IconSearch } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   小队 /resources/squads
   对照 hercules-frontend SquadsPage：squad-grid 卡片 + 新建小队弹层。
   本地后端尚未提供小队接口，列表恒为空，不伪造数据。
   ------------------------------------------------------------------ */

interface SquadRow {
  id: string
  name: string
  description: string
  memberCount: number
  status: 'enabled' | 'disabled'
  lastActive: string
}

/** 本地后端尚未提供小队能力，接口接入前列表恒为空。 */
const SQUADS: SquadRow[] = []

const SQUAD_STATUS: Record<SquadRow['status'], { label: string; tone: string }> = {
  enabled: { label: '启用', tone: 'done' },
  disabled: { label: '停用', tone: 'muted' },
}

function SquadCard({ squad }: { squad: SquadRow }) {
  const meta = SQUAD_STATUS[squad.status]
  return (
    <article className="squad-card">
      <div className="squad-card-head">
        <strong>{squad.name}</strong>
        <StateTag tone={meta.tone}>{meta.label}</StateTag>
      </div>
      <p>{squad.description || '暂无小队描述'}</p>
      <div className="squad-meta">
        <span>成员 Agent：{squad.memberCount}</span>
        <span>最近活跃：{squad.lastActive || '暂无数据'}</span>
      </div>
      <div className="machine-foot">
        <button
          type="button"
          className="resource-action"
          onClick={() => toast('本地后端尚未提供该能力，无法查看小队详情。')}
        >
          查看
        </button>
        <span className="squad-card-actions">
          <button
            type="button"
            className="resource-action"
            onClick={() => toast('本地后端尚未提供该能力，无法编辑小队。')}
          >
            编辑
          </button>
          <span className="resource-action-divider">|</span>
          <button
            type="button"
            className="resource-action danger"
            onClick={() => toast('本地后端尚未提供该能力，无法删除小队。')}
          >
            删除
          </button>
        </span>
      </div>
    </article>
  )
}

export function CreateSquadModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [status, setStatus] = useState<'enabled' | 'disabled'>('enabled')
  const [description, setDescription] = useState('')
  const [notice, setNotice] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setNotice('请输入小队名称。')
      return
    }
    if (trimmed.length > 64) {
      setNotice('小队名称不能超过64个字符。')
      return
    }
    setNotice('本地后端尚未提供该能力，创建小队不会写入数据库。')
    toast(`本地后端尚未提供该能力，未能创建小队：${trimmed}。`)
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal squad-editor-modal" role="dialog" aria-modal="true" aria-label="新建小队">
        <form onSubmit={submit}>
          <div className="modal-head">
            <div>
              <h3>新建小队</h3>
              <p className="resource-page-note" style={{ marginTop: 4 }}>
                组织协作成员，约定分工、交接方式与确认规则。
              </p>
            </div>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="resource-modal-form">
              <section className="squad-editor-section">
                <h4>基础信息</h4>
                <div className="form-grid">
                  <label className="field">
                    <span>小队名称</span>
                    <input
                      value={name}
                      maxLength={64}
                      placeholder="例如：需求方案评审小队"
                      onChange={(event) => setName(event.target.value)}
                    />
                  </label>
                  <label className="field">
                    <span>状态</span>
                    <select value={status} onChange={(event) => setStatus(event.target.value as 'enabled' | 'disabled')}>
                      <option value="enabled">启用</option>
                      <option value="disabled">停用</option>
                    </select>
                  </label>
                  <label className="field wide">
                    <span>小队描述（选填）</span>
                    <textarea
                      value={description}
                      rows={3}
                      placeholder="说明小队负责的工作，方便在创建 Issue 时选择"
                      onChange={(event) => setDescription(event.target.value)}
                    />
                  </label>
                </div>
              </section>

              <section className="squad-editor-section">
                <h4>小队成员</h4>
                <p className="squad-editor-hint">
                  负责人统筹任务分派与结果汇总，小队中的特殊分工请在「协作指令」中说明。
                </p>
                <div className="form-grid">
                  <label className="field">
                    <span>负责人 Agent</span>
                    <select value="" disabled>
                      <option value="">请选择负责协调小队的 Agent</option>
                    </select>
                    <small className="field-help">本地后端尚未提供该能力，暂无可选 Agent。</small>
                  </label>
                </div>

                <div className="resource-section-title" style={{ marginTop: 14 }}>
                  协作成员
                  <button
                    type="button"
                    className="button ghost small"
                    onClick={() => toast('本地后端尚未提供该能力，暂无可添加的 Agent 或人类成员。')}
                  >
                    <IconPlus width={14} height={14} /> 添加成员
                  </button>
                </div>
                <div className="empty-inline">
                  <EmptyState
                    title="还未添加协作成员"
                    description="添加 Agent 承担方案、Review 或开发工作，也可添加人类成员参与确认。本地后端尚未提供该能力。"
                    action={
                      <button
                        type="button"
                        className="button ghost"
                        onClick={() => toast('本地后端尚未提供该能力，暂无可添加的 Agent 或人类成员。')}
                      >
                        添加成员
                      </button>
                    }
                  />
                </div>
              </section>

              {notice ? (
                <p className="notice warn" role="status">
                  {notice}
                </p>
              ) : null}
            </div>
          </div>
          <div className="modal-foot">
            <div className="squad-editor-footer">
              <span className="squad-editor-footer-hint">创建后，可在 Issue 中选择此小队。</span>
              <span className="resource-actions">
                <button type="button" className="button subtle" onClick={onClose}>
                  取消
                </button>
                <button type="submit" className="button primary">
                  创建小队
                </button>
              </span>
            </div>
          </div>
        </form>
      </div>
    </div>
  )
}

export default function Squads() {
  const [keyword, setKeyword] = useState('')
  const [createOpen, setCreateOpen] = useState(false)

  const filtered = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    if (!query) return SQUADS
    return SQUADS.filter((squad) => [squad.name, squad.description].join(' ').toLowerCase().includes(query))
  }, [keyword])

  return (
    <AppShell
      title="小队"
      breadcrumb="资源管理 / 小队"
      actions={
        <button type="button" className="button ghost" onClick={() => toast('本地后端尚未提供该能力，无法刷新小队列表。')}>
          <IconRefresh width={15} height={15} /> 刷新
        </button>
      }
    >
      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>小队</h2>
            <p>组织协作成员，约定分工、交接方式与确认规则。</p>
          </div>
          <div className="panel-actions">
            <label className="resource-search">
              <IconSearch width={15} height={15} />
              <input
                value={keyword}
                placeholder="搜索小队名称或描述"
                aria-label="搜索小队"
                onChange={(event) => setKeyword(event.target.value)}
              />
            </label>
            <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
              <IconPlus width={15} height={15} /> 创建小队
            </button>
          </div>
        </div>

        {filtered.length ? (
          <div className="squad-grid">
            {filtered.map((squad) => (
              <SquadCard key={squad.id} squad={squad} />
            ))}
          </div>
        ) : (
          <div className="empty-inline">
            <EmptyState
              title={keyword.trim() ? '没有匹配的小队' : '当前用户暂无小队'}
              description={
                keyword.trim()
                  ? '没有找到符合搜索条件的小队，请调整关键词后重试。'
                  : '本地后端尚未提供该能力。页面结构按阿拉丁设计还原，接入小队接口后即可展示成员与协作配置。'
              }
              action={
                keyword.trim() ? (
                  <button type="button" className="button ghost" onClick={() => setKeyword('')}>
                    清空搜索
                  </button>
                ) : (
                  <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                    创建小队
                  </button>
                )
              }
            />
          </div>
        )}
      </section>

      {createOpen ? <CreateSquadModal onClose={() => setCreateOpen(false)} /> : null}
    </AppShell>
  )
}
