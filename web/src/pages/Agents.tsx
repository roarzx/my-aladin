import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'

import {
  createAgent,
  fetchAgents,
  fetchMachines,
  fetchWorkspaces,
  updateAgent,
  type Agent,
  type Machine,
} from '../api/controlplane'
import { ApiError } from '../api/issues'
import AppShell from '../components/AppShell'
import { IconPlus, IconRefresh, IconSearch } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   Agent 管理 /agents
   真实数据：GET /workspaces → GET /agents?workspaceId= 与 /machines?workspaceId=；
   创建 / 编辑调用 POST /agents、PATCH /agents/{id}。
   ------------------------------------------------------------------ */

const MACHINE_STATUS: Record<string, { label: string; tone: string }> = {
  online: { label: '在线', tone: 'done' },
  busy: { label: '忙碌', tone: 'running' },
  offline: { label: '离线', tone: 'muted' },
}

function machineStatusMeta(status: string): { label: string; tone: string } {
  return MACHINE_STATUS[status] ?? { label: status || '未知', tone: 'muted' }
}

function isAbsolutePath(value: string): boolean {
  return value.startsWith('/') || /^[A-Za-z]:[\\/]/.test(value)
}

function AgentCard({ agent, machine }: { agent: Agent; machine?: Machine }) {
  const meta = machine ? machineStatusMeta(machine.status) : { label: '未绑定', tone: 'muted' }
  return (
    <Link
      className={`agent-card ${agent.enabled ? 'idle' : 'offline'}`}
      to={`/agent-detail?id=${encodeURIComponent(agent.id)}`}
    >
      <div className="agent-card-head">
        <strong>
          {agent.name}
          {agent.visibility === 'shared' ? <em className="shared-badge">共享</em> : null}
        </strong>
        <StateTag tone={agent.enabled ? 'done' : 'muted'}>{agent.enabled ? '已启用' : '已停用'}</StateTag>
      </div>
      <p>
        绑定机器：{machine ? `${machine.name}（${meta.label}）` : '未绑定可用机器'}
      </p>
      <p>工作目录：{agent.workDir || '暂无数据'}</p>
      <p>模型：{agent.model || '跟随 Agent 默认 Model'}</p>
      <div className="machine-foot">
        <span>{agent.visibility === 'shared' ? '共享' : '私有'}</span>
        <span>详情</span>
      </div>
    </Link>
  )
}

export function AgentFormModal({
  workspaceId,
  machines,
  agent,
  defaultMachineId,
  onClose,
  onSaved,
}: {
  workspaceId: string
  machines: Machine[]
  agent: Agent | null
  defaultMachineId?: string
  onClose: () => void
  onSaved: () => void
}) {
  const editing = Boolean(agent)
  const [name, setName] = useState(agent?.name ?? '')
  const [machineId, setMachineId] = useState(agent?.machineId ?? defaultMachineId ?? '')
  const [workDir, setWorkDir] = useState(agent?.workDir ?? '')
  const [systemPrompt, setSystemPrompt] = useState(agent?.systemPrompt ?? '')
  const [model, setModel] = useState(agent?.model ?? '')
  const [visibility, setVisibility] = useState(agent?.visibility === 'shared' ? 'shared' : 'private')
  const [enabled, setEnabled] = useState(agent?.enabled ?? true)
  const [notice, setNotice] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const trimmedName = name.trim()
    if (!trimmedName) {
      setNotice('请输入 Agent 名称。')
      return
    }
    if (trimmedName.length > 64) {
      setNotice('Agent 名称不能超过64个字符。')
      return
    }
    if (!machineId) {
      setNotice('请选择要绑定的机器。')
      return
    }
    const trimmedDir = workDir.trim()
    if (!trimmedDir) {
      setNotice('请输入工作目录。')
      return
    }
    if (!isAbsolutePath(trimmedDir)) {
      setNotice('工作目录必须是绝对路径，例如 /home/user/project。')
      return
    }

    const body = {
      name: trimmedName,
      machineId,
      workDir: trimmedDir,
      systemPrompt: systemPrompt.trim(),
      visibility,
      model: model.trim(),
      enabled,
    }

    setSubmitting(true)
    setNotice('')
    try {
      if (agent) {
        await updateAgent(agent.id, body)
        toast(`已更新 Agent：${trimmedName}`)
      } else {
        await createAgent({ workspaceId, ...body })
        toast(`已创建 Agent：${trimmedName}`)
      }
      onSaved()
      onClose()
    } catch (err) {
      setNotice(err instanceof ApiError ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
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
        aria-label={editing ? '编辑 Agent' : '创建 Agent'}
        style={{ width: 'min(760px, 100%)' }}
      >
        <form onSubmit={(event) => void submit(event)}>
          <div className="modal-head">
            <h3>{editing ? '编辑 Agent' : '创建 Agent'}</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="resource-modal-form">
              <p className="resource-page-note">
                Agent 绑定一台已上线机器的工作目录，daemon 在该目录下启动 Claude Code 执行任务步骤。
              </p>

              <section className="resource-section">
                <h4 className="resource-section-title">基础信息</h4>
                <div className="form-grid">
                  <label className="field">
                    <span>Agent 名称</span>
                    <input
                      value={name}
                      maxLength={64}
                      placeholder="请输入 Agent 名称，如 Claude Code (本地)"
                      onChange={(event) => setName(event.target.value)}
                    />
                    <small className="field-help">名称用于在任务与小队中识别该 Agent。</small>
                  </label>

                  <label className="field">
                    <span>绑定机器</span>
                    <select value={machineId} onChange={(event) => setMachineId(event.target.value)}>
                      <option value="">{machines.length ? '请选择机器' : '暂无可用机器'}</option>
                      {machines.map((machine) => (
                        <option key={machine.id} value={machine.id}>
                          {machine.name} · {machineStatusMeta(machine.status).label}
                        </option>
                      ))}
                    </select>
                    <small className="field-help">
                      {machines.length ? '建议选择「在线」的机器。' : '请先在机器管理页注册并启动 daemon。'}
                    </small>
                  </label>

                  <label className="field wide">
                    <span>工作目录</span>
                    <input
                      value={workDir}
                      placeholder="/home/user/project"
                      onChange={(event) => setWorkDir(event.target.value)}
                    />
                    <small className="field-help">机器上的绝对路径，Agent 在该目录下执行任务。</small>
                  </label>

                  <label className="field">
                    <span>使用范围</span>
                    <div className="agent-usage-scope-options" role="radiogroup" aria-label="使用范围">
                      <label>
                        <input
                          type="radio"
                          name="usage-scope"
                          checked={visibility === 'private'}
                          onChange={() => setVisibility('private')}
                        />{' '}
                        私有
                      </label>
                      <label>
                        <input
                          type="radio"
                          name="usage-scope"
                          checked={visibility === 'shared'}
                          onChange={() => setVisibility('shared')}
                        />{' '}
                        共享
                      </label>
                    </div>
                    <small className="field-help">私有 Agent 仅自己可见；共享后小队成员也可使用。</small>
                  </label>

                  <label className="field">
                    <span>默认 Model</span>
                    <div className="model-input-wrap">
                      <input
                        value={model}
                        placeholder="请输入或选择 Model"
                        onChange={(event) => setModel(event.target.value)}
                      />
                    </div>
                    <small className="field-help">留空时使用 Agent 启动时的默认 Model。</small>
                  </label>

                  <label className="field">
                    <span>启用状态</span>
                    <div className="agent-usage-scope-options">
                      <label>
                        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
                        启用该 Agent
                      </label>
                    </div>
                    <small className="field-help">停用后该 Agent 不会参与任务调度。</small>
                  </label>
                </div>
              </section>

              <section className="resource-section">
                <h4 className="resource-section-title">System Prompt</h4>
                <label className="field wide">
                  <span>System Prompt</span>
                  <textarea
                    className="system-prompt-input"
                    rows={6}
                    value={systemPrompt}
                    placeholder="填写该 Agent 的系统提示词，例如项目约定、代码风格与禁止事项。"
                    onChange={(event) => setSystemPrompt(event.target.value)}
                  />
                  <small className="field-help">留空时使用平台默认提示词。</small>
                </label>
              </section>

              {notice ? (
                <p className="notice danger" role="alert">
                  {notice}
                </p>
              ) : null}
            </div>
          </div>
          <div className="modal-foot">
            <button type="button" className="button subtle" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="button primary" disabled={submitting}>
              {submitting ? '保存中…' : editing ? '保存修改' : '确认创建'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default function Agents() {
  const [workspaceId, setWorkspaceId] = useState('')
  const [agents, setAgents] = useState<Agent[]>([])
  const [machines, setMachines] = useState<Machine[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [keyword, setKeyword] = useState('')
  const [createOpen, setCreateOpen] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      let wid = workspaceId
      if (!wid) {
        const workspaces = await fetchWorkspaces()
        if (workspaces.length === 0) {
          setAgents([])
          setMachines([])
          setLoading(false)
          return
        }
        wid = workspaces[0].id
        setWorkspaceId(wid)
      }
      const [agentList, machineList] = await Promise.all([fetchAgents(wid), fetchMachines(wid)])
      setAgents(agentList)
      setMachines(machineList)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspaceId])

  useEffect(() => {
    void load()
  }, [load])

  const machineName = useMemo(() => {
    const map = new Map<string, string>()
    for (const machine of machines) map.set(machine.id, machine.name)
    return map
  }, [machines])

  const filtered = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    if (!query) return agents
    return agents.filter((agent) =>
      [agent.name, agent.workDir, agent.model, machineName.get(agent.machineId) ?? '']
        .join(' ')
        .toLowerCase()
        .includes(query),
    )
  }, [agents, keyword, machineName])

  return (
    <AppShell
      title="Agent 运行控制"
      breadcrumb="Agent 管理"
      actions={
        <button type="button" className="button ghost" onClick={() => void load()}>
          <IconRefresh width={15} height={15} /> 刷新
        </button>
      }
    >
      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>Agent 管理</h2>
            <p>创建 Agent 配置时可绑定到指定机器，系统将同时创建 Agent 实例。</p>
          </div>
          <div className="panel-actions">
            <label className="resource-search">
              <IconSearch width={15} height={15} />
              <input
                value={keyword}
                placeholder="搜索 Agent 名称、机器或模型"
                aria-label="搜索 Agent"
                onChange={(event) => setKeyword(event.target.value)}
              />
            </label>
            <button
              type="button"
              className="button primary"
              onClick={() => setCreateOpen(true)}
              disabled={!workspaceId}
            >
              <IconPlus width={15} height={15} /> 创建 Agent
            </button>
          </div>
        </div>

        {error ? (
          <p className="notice danger" role="alert">
            加载失败:{error}
          </p>
        ) : null}

        {loading ? (
          <div className="empty-inline">加载中…</div>
        ) : filtered.length ? (
          <div className="agent-board">
            {filtered.map((agent) => (
              <AgentCard
                key={agent.id}
                agent={agent}
                machine={machines.find((item) => item.id === agent.machineId)}
              />
            ))}
          </div>
        ) : (
          <div className="empty-inline">
            <EmptyState
              title={keyword.trim() ? '没有匹配的 Agent' : '当前用户暂无 Agent'}
              description={
                keyword.trim()
                  ? '没有找到符合搜索条件的 Agent，请调整关键词后重试。'
                  : '创建 Agent 并绑定已上线的机器后，即可在创建任务时选择它执行步骤。'
              }
              action={
                keyword.trim() ? (
                  <button type="button" className="button ghost" onClick={() => setKeyword('')}>
                    清空搜索
                  </button>
                ) : (
                  <button
                    type="button"
                    className="button ghost"
                    onClick={() => setCreateOpen(true)}
                    disabled={!workspaceId}
                  >
                    创建 Agent
                  </button>
                )
              }
            />
          </div>
        )}
      </section>

      {createOpen ? (
        <AgentFormModal
          workspaceId={workspaceId}
          machines={machines}
          agent={null}
          onClose={() => setCreateOpen(false)}
          onSaved={() => void load()}
        />
      ) : null}
    </AppShell>
  )
}
