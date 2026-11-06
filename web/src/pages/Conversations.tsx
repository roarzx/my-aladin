import { useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

import AppShell from '../components/AppShell'
import { EmptyState, Panel, StateTag, toast } from '../components/ui'
import '../styles/pages-collab.css'

interface LocalConversation {
  id: string
  title: string
  createdAt: string
  updatedAt: string
}

interface HistoryMenu {
  conversationId: string
  x: number
  y: number
}

const UNTITLED = '未命名对话'
const HISTORY_GROUPS = ['今天', '昨天', '近 7 天', '更早'] as const

function pad(value: number) {
  return String(value).padStart(2, '0')
}

function formatDateTime(iso: string) {
  const date = new Date(iso)
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function historyGroup(iso: string): (typeof HISTORY_GROUPS)[number] {
  const date = new Date(iso)
  const now = new Date()
  const dayStart = (value: Date) => new Date(value.getFullYear(), value.getMonth(), value.getDate()).getTime()
  const diffDays = Math.floor((dayStart(now) - dayStart(date)) / 86_400_000)
  if (diffDays <= 0) return '今天'
  if (diffDays === 1) return '昨天'
  if (diffDays < 7) return '近 7 天'
  return '更早'
}

function IconPencil() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" />
    </svg>
  )
}

function IconPaperclip() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="m21.4 11.05-8.49 8.49a6 6 0 0 1-8.48-8.49l8.49-8.48a4 4 0 0 1 5.66 5.65l-8.49 8.49a2 2 0 0 1-2.83-2.83l8.49-8.48" />
    </svg>
  )
}

function IconTool() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.3 2.3-2.3-2.3 2.3-2.3Z" />
    </svg>
  )
}

export default function Conversations() {
  const [conversations, setConversations] = useState<LocalConversation[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [historyOpen, setHistoryOpen] = useState(true)
  const [inspectorOpen, setInspectorOpen] = useState(false)
  const [menu, setMenu] = useState<HistoryMenu | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [titleDraft, setTitleDraft] = useState('')
  const [composer, setComposer] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<LocalConversation | null>(null)
  const nextId = useRef(1)

  const active = conversations.find((item) => item.id === activeId) ?? null

  const grouped = useMemo(() => {
    const result = new Map<string, LocalConversation[]>()
    HISTORY_GROUPS.forEach((group) => result.set(group, []))
    conversations.forEach((item) => {
      result.get(historyGroup(item.updatedAt))?.push(item)
    })
    return result
  }, [conversations])

  useEffect(() => {
    if (!menu && !inspectorOpen && !deleteTarget) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setMenu(null)
      setInspectorOpen(false)
      setDeleteTarget(null)
    }
    const handleClick = () => setMenu(null)
    window.addEventListener('keydown', handleKeyDown)
    window.addEventListener('click', handleClick)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('click', handleClick)
    }
  }, [menu, inspectorOpen, deleteTarget])

  function createConversation() {
    const now = new Date().toISOString()
    const conversation: LocalConversation = {
      id: `local-${nextId.current++}`,
      title: UNTITLED,
      createdAt: now,
      updatedAt: now,
    }
    setConversations((prev) => [conversation, ...prev])
    setActiveId(conversation.id)
    setHistoryOpen(true)
    setMenu(null)
  }

  function startEditing(conversation: LocalConversation) {
    setEditingId(conversation.id)
    setTitleDraft(conversation.title)
    setMenu(null)
  }

  function cancelEditing() {
    setEditingId(null)
    setTitleDraft('')
  }

  function saveTitle() {
    const nextTitle = titleDraft.trim()
    if (!nextTitle || !editingId) return
    setConversations((prev) =>
      prev.map((item) =>
        item.id === editingId ? { ...item, title: nextTitle, updatedAt: new Date().toISOString() } : item,
      ),
    )
    cancelEditing()
  }

  function requestDelete(conversation: LocalConversation) {
    setDeleteTarget(conversation)
    setMenu(null)
  }

  function confirmDelete() {
    if (!deleteTarget) return
    setConversations((prev) => prev.filter((item) => item.id !== deleteTarget.id))
    if (activeId === deleteTarget.id) setActiveId(null)
    if (editingId === deleteTarget.id) cancelEditing()
    setDeleteTarget(null)
    toast('本地后端尚未提供对话删除接口，已移除本地草稿')
  }

  function handleSend() {
    if (!composer.trim()) return
    toast('本地后端尚未提供对话接口')
  }

  function handleComposerKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      handleSend()
    }
  }

  return (
    <AppShell title="多 Agent 对话" breadcrumb="对话">
      <Panel className={`conversation-page${historyOpen ? '' : ' history-collapsed'}`}>
        <aside className="conversation-page-sidebar" aria-label="历史对话">
          <div className="conversation-page-sidebar-head">
            <span className="section-label">历史对话</span>
            <button type="button" className="button subtle small" onClick={createConversation}>
              新建对话
            </button>
          </div>
          <div className="conversation-history-list">
            {conversations.length === 0 ? (
              <div className="empty-inline">暂无对话内容</div>
            ) : (
              HISTORY_GROUPS.map((group) => {
                const items = grouped.get(group) ?? []
                if (items.length === 0) return null
                return (
                  <div className="conversation-history-group" key={group}>
                    <div className="conversation-history-group-label">{group}</div>
                    {items.map((item) => {
                      const editing = editingId === item.id
                      return (
                        <div
                          key={item.id}
                          className={[
                            'conversation-history-item',
                            item.id === activeId ? 'active' : '',
                            editing ? 'editable' : '',
                            'draft',
                          ]
                            .filter(Boolean)
                            .join(' ')}
                          onContextMenu={(event) => {
                            event.preventDefault()
                            setMenu({ conversationId: item.id, x: event.clientX, y: event.clientY })
                          }}
                        >
                          {editing ? (
                            <div className="conversation-history-editor">
                              <input
                                className="input"
                                value={titleDraft}
                                maxLength={255}
                                autoFocus
                                placeholder="请输入会话标题"
                                onChange={(event) => setTitleDraft(event.target.value)}
                                onKeyDown={(event) => {
                                  if (event.key === 'Enter') {
                                    event.preventDefault()
                                    saveTitle()
                                  }
                                  if (event.key === 'Escape') cancelEditing()
                                }}
                              />
                              <button type="button" className="button small" onClick={saveTitle}>
                                保存
                              </button>
                              <button type="button" className="button ghost small" onClick={cancelEditing}>
                                取消
                              </button>
                            </div>
                          ) : (
                            <>
                              <button
                                type="button"
                                className="conversation-history-main"
                                onClick={() => setActiveId(item.id)}
                              >
                                <span className="conversation-history-top">
                                  <span className="conversation-history-title">{item.title || UNTITLED}</span>
                                  <span className="conversation-history-badges">
                                    <span className="conversation-history-badge draft">草稿</span>
                                  </span>
                                </span>
                                <span className="conversation-history-preview">暂无对话内容</span>
                                <span className="conversation-history-meta">
                                  <span className="conversation-history-meta-right">
                                    {formatDateTime(item.updatedAt)}
                                  </span>
                                </span>
                              </button>
                              <button
                                type="button"
                                className="conversation-history-edit"
                                aria-label="编辑会话标题"
                                title="编辑会话标题"
                                onClick={() => startEditing(item)}
                              >
                                <IconPencil />
                              </button>
                            </>
                          )}
                        </div>
                      )
                    })}
                  </div>
                )
              })
            )}
          </div>
        </aside>

        <div className="conversation-page-main">
          <div className="cockpit-header conversation-page-header">
            <div className="conversation-page-heading">
              <div className="conversation-page-title-row">
                <h2>{active ? active.title || UNTITLED : UNTITLED}</h2>
                {active ? <StateTag tone="draft">草稿</StateTag> : null}
              </div>
              <span className="conversation-page-summary">0 Agent · 0 知识库</span>
            </div>
            <div className="conversation-page-actions">
              <button type="button" className="button primary conversation-create-trigger" onClick={createConversation}>
                新建对话
              </button>
              <button
                type="button"
                className="button conversation-history-trigger"
                aria-pressed={historyOpen}
                onClick={() => setHistoryOpen((open) => !open)}
              >
                历史对话
              </button>
              <button
                type="button"
                className="button conversation-inspector-trigger"
                disabled={!active}
                onClick={() => setInspectorOpen(true)}
              >
                会话信息
              </button>
              {active ? (
                <button
                  type="button"
                  className="button danger conversation-delete-trigger"
                  onClick={() => requestDelete(active)}
                >
                  删除对话
                </button>
              ) : null}
            </div>
          </div>

          <div className="chat-panel conversation-page-chat">
            <div className="conversation-messages">
              {active ? (
                <EmptyState
                  title="暂无对话内容"
                  description="输入 @Agent 可定向发送；未点名时会广播给当前会话里的全部 Agent。"
                />
              ) : (
                <EmptyState
                  title="从一次探索式对话开始"
                  description="输入 @Agent 可定向发送；未点名时会广播给当前会话里的全部 Agent。"
                  action={
                    <button type="button" className="button primary" onClick={createConversation}>
                      新建对话
                    </button>
                  }
                />
              )}
            </div>

            <div className="composer">
              <div className="composer-toolbar">
                <div className="composer-toolbar-leading">
                  <button
                    type="button"
                    className="composer-tool-button composer-attachment-button"
                    title="添加附件"
                    aria-label="添加附件"
                    onClick={() => toast('本地后端尚未提供附件上传接口')}
                  >
                    <IconPaperclip />
                  </button>
                  <button
                    type="button"
                    className="composer-tool-button"
                    title="工具"
                    aria-label="工具"
                    onClick={() => toast('本地后端尚未提供工具调用接口')}
                  >
                    <IconTool />
                  </button>
                </div>
                <span className="composer-hint">按 Enter 发送，Shift + Enter 换行</span>
              </div>
              <textarea
                className="input"
                rows={3}
                value={composer}
                maxLength={2000}
                placeholder="向当前会话发送消息，输入 @ 可定向到某个 Agent"
                onChange={(event) => setComposer(event.target.value)}
                onKeyDown={handleComposerKeyDown}
              />
              <div className="composer-actions">
                <span className="composer-count">{composer.length}/2000</span>
                <button
                  type="button"
                  className="button primary composer-send"
                  disabled={!composer.trim()}
                  onClick={handleSend}
                >
                  发送
                </button>
              </div>
            </div>
          </div>
        </div>
      </Panel>

      {menu ? (
        <div
          className="conversation-history-context-menu"
          style={{ left: menu.x, top: menu.y }}
          role="menu"
          onClick={(event) => event.stopPropagation()}
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setMenu(null)
              toast('本地后端尚未提供对话分享接口')
            }}
          >
            分享
          </button>
          <button
            type="button"
            role="menuitem"
            className="danger"
            onClick={() => {
              const target = conversations.find((item) => item.id === menu.conversationId)
              if (target) requestDelete(target)
            }}
          >
            删除
          </button>
        </div>
      ) : null}

      {inspectorOpen && active ? (
        <div className="conversation-inspector-overlay" onClick={() => setInspectorOpen(false)}>
          <div
            className="conversation-inspector-panel"
            role="dialog"
            aria-modal="true"
            aria-label="会话信息"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="conversation-inspector-head">
              <strong>会话信息</strong>
              <button type="button" className="button ghost" onClick={() => setInspectorOpen(false)}>
                关闭
              </button>
            </div>
            <div className="conversation-inspector-body">
              <section className="conversation-inspector-section">
                <div className="conversation-inspector-title">会话概览</div>
                <div className="conversation-inspector-meta">
                  <span className="label">标题</span>
                  {editingId === active.id ? (
                    <div className="conversation-title-editor">
                      <input
                        className="input conversation-title-input"
                        value={titleDraft}
                        maxLength={255}
                        placeholder="请输入会话标题"
                        onChange={(event) => setTitleDraft(event.target.value)}
                      />
                      <div className="conversation-title-editor-actions">
                        <button type="button" className="button small" onClick={saveTitle}>
                          保存
                        </button>
                        <button type="button" className="button ghost small" onClick={cancelEditing}>
                          取消
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="conversation-title-display">
                      <strong>{active.title || UNTITLED}</strong>
                      <button
                        type="button"
                        className="conversation-title-edit"
                        aria-label="编辑会话标题"
                        onClick={() => startEditing(active)}
                      >
                        <IconPencil />
                      </button>
                    </div>
                  )}
                  <span className="label">状态</span>
                  <strong>草稿</strong>
                  <span className="label">创建时间</span>
                  <strong>{active.createdAt ? formatDateTime(active.createdAt) : '尚未创建'}</strong>
                  <span className="label">最近更新</span>
                  <strong>{active.updatedAt ? formatDateTime(active.updatedAt) : '尚未更新'}</strong>
                </div>
              </section>

              <section className="conversation-inspector-section">
                <div className="conversation-inspector-title">参与 Agent</div>
                <select className="input" disabled defaultValue="">
                  <option value="">选择参与当前对话的 Agent</option>
                </select>
                <div className="conversation-inspector-empty">尚未添加参与 Agent</div>
              </section>

              <section className="conversation-inspector-section">
                <div className="conversation-inspector-title">知识库</div>
                <select className="input" disabled defaultValue="">
                  <option value="">绑定当前对话使用的知识库</option>
                </select>
                <div className="conversation-inspector-empty">尚未绑定知识库</div>
              </section>

              <section className="conversation-inspector-section">
                <div className="conversation-inspector-title">预留区域</div>
                <div className="conversation-inspector-empty">后续可扩展会话摘要、附件、引用上下文等能力。</div>
              </section>
            </div>
          </div>
        </div>
      ) : null}

      {deleteTarget ? (
        <div className="modal-backdrop" onClick={() => setDeleteTarget(null)}>
          <div
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label="删除对话"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-head">
              <h3>删除对话</h3>
              <button type="button" className="button ghost small" onClick={() => setDeleteTarget(null)}>
                关闭
              </button>
            </div>
            <div className="modal-body">
              <p>确认删除「{deleteTarget.title || UNTITLED}」？该操作不可撤销。</p>
              <div className="notice warn">本地后端尚未提供对话接口，删除仅移除本地草稿。</div>
            </div>
            <div className="modal-foot">
              <button type="button" className="button" onClick={() => setDeleteTarget(null)}>
                取消
              </button>
              <button type="button" className="button danger" onClick={confirmDelete}>
                删除
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </AppShell>
  )
}
