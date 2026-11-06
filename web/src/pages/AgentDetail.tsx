import { useCallback, useEffect, useState } from 'react'

import {
  disableAgent,
  fetchAgents,
  fetchMachines,
  fetchWorkspaces,
  type Agent,
  type Machine,
} from '../api/controlplane'
import { ApiError } from '../api/issues'
import AppShell from '../components/AppShell'
import { IconArrowLeft, IconRefresh } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link, useQueryParam } from '../router'
import { AgentFormModal } from './Agents'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   Agent 详情 /agent-detail?id=…
   后端暂无 GET /agents/{id}，这里按 workspace 拉取列表后按 id 定位。
   展示真实配置，并支持编辑（PATCH）与停用（DELETE）。
   ------------------------------------------------------------------ */

const MACHINE_STATUS: Record<string, { label: string; tone: string }> = {
  online: { label: '在线', tone: 'done' },
  busy: { label: '忙碌', tone: 'running' },
  offline: { label: '离线', tone: 'muted' },
}

function machineStatusMeta(status: string): { label: string; tone: string } {
  return MACHINE_STATUS[status] ?? { label: status || '未知', tone: 'muted' }
}

const monospace = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' }

function jsonText(value: unknown): string {
  if (value === null || value === undefined) return '暂无配置'
  if (typeof value === 'object' && Object.keys(value as Record<string, unknown>).length === 0) return '暂无配置'
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export default function AgentDetail() {
  const agentId = useQueryParam('id')
  const [agent, setAgent] = useState<Agent | null>(null)
  const [machines, setMachines] = useState<Machine[]>([])
  const [workspaceId, setWorkspaceId] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    if (!agentId) {
      setLoading(false)
      return
    }
    setError('')
    try {
      const workspaces = await fetchWorkspaces()
      if (workspaces.length === 0) {
        setAgent(null)
        setLoading(false)
        return
      }
      const wid = workspaces[0].id
      setWorkspaceId(wid)
      const [agentList, machineList] = await Promise.all([fetchAgents(wid), fetchMachines(wid)])
      setMachines(machineList)
      setAgent(agentList.find((item) => item.id === agentId) ?? null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => {
    void load()
  }, [load])

  const disable = async () => {
    if (!agent) return
    setSaving(true)
    setError('')
    try {
      const updated = await disableAgent(agent.id)
      setAgent(updated)
      toast(`已停用 Agent：${agent.name}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (!agentId) {
    return (
      <AppShell
        title="Agent 详情"
        breadcrumb="Agent 管理 / Agent 详情"
        actions={
          <Link className="button ghost" to="/agents">
            <IconArrowLeft width={15} height={15} /> 返回 Agent 列表
          </Link>
        }
      >
        <section className="panel">
          <div className="empty-inline">
            <EmptyState
              title="未选择 Agent"
              description="请从 Agent 管理列表选择一个 Agent 查看详情。"
              action={
                <Link className="button primary" to="/agents">
                  前往 Agent 管理
                </Link>
              }
            />
          </div>
        </section>
      </AppShell>
    )
  }

  const machine = agent ? machines.find((item) => item.id === agent.machineId) : undefined
  const machineMeta = machine ? machineStatusMeta(machine.status) : { label: '未绑定', tone: 'muted' }

  return (
    <AppShell
      title="Agent 详情"
      breadcrumb={
        <>
          <Link to="/agents">Agent 管理</Link> / Agent 详情
        </>
      }
      actions={
        <>
          <button type="button" className="button ghost" onClick={() => void load()}>
            <IconRefresh width={15} height={15} /> 刷新
          </button>
          <Link className="button subtle" to="/agents">
            <IconArrowLeft width={15} height={15} /> 返回列表
          </Link>
        </>
      }
    >
      {error ? (
        <p className="notice danger" role="alert">
          加载失败:{error}
        </p>
      ) : null}

      {loading ? (
        <section className="panel">
          <div className="empty-inline">加载中…</div>
        </section>
      ) : !agent ? (
        <section className="panel">
          <div className="empty-inline">
            <EmptyState
              title="未找到该 Agent"
              description="该 Agent 可能已被删除，或不属于当前工作区。"
              action={
                <Link className="button primary" to="/agents">
                  返回 Agent 管理
                </Link>
              }
            />
          </div>
        </section>
      ) : (
        <>
          <section className="panel">
            <div className="panel-head">
              <div>
                <h2>
                  {agent.name}{' '}
                  <StateTag tone={agent.enabled ? 'done' : 'muted'}>
                    {agent.enabled ? '已启用' : '已停用'}
                  </StateTag>
                </h2>
                <p>
                  Agent ID：<code style={monospace}>{agent.id}</code>
                </p>
              </div>
              <div className="panel-actions">
                <button type="button" className="button ghost" onClick={() => setEditing(true)}>
                  编辑配置
                </button>
                <button
                  type="button"
                  className="button subtle"
                  disabled={!agent.enabled || saving}
                  onClick={() => void disable()}
                >
                  {agent.enabled ? '停用 Agent' : '已停用'}
                </button>
              </div>
            </div>
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>基本信息</span>
            </div>
            <div className="detail-grid">
              <div className="detail-card">
                <span>Agent 名称</span>
                <strong>{agent.name}</strong>
              </div>
              <div className="detail-card">
                <span>运行时类型</span>
                <strong>{agent.runtimeType || '暂无数据'}</strong>
              </div>
              <div className="detail-card">
                <span>状态</span>
                <strong>{agent.enabled ? '已启用' : '已停用'}</strong>
              </div>
              <div className="detail-card">
                <span>使用范围</span>
                <strong>{agent.visibility === 'shared' ? '共享' : '私有'}</strong>
              </div>
              <div className="detail-card">
                <span>所在机器</span>
                <strong>
                  {machine ? (
                    <>
                      {machine.name} <StateTag tone={machineMeta.tone}>{machineMeta.label}</StateTag>
                    </>
                  ) : (
                    '未绑定可用机器'
                  )}
                </strong>
              </div>
              <div className="detail-card">
                <span>工作目录</span>
                <strong style={monospace}>{agent.workDir || '暂无数据'}</strong>
              </div>
            </div>
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>模型配置</span>
            </div>
            <div className="detail-grid">
              <div className="detail-card">
                <span>默认 Model</span>
                <strong>{agent.model || '跟随 Agent 默认 Model'}</strong>
              </div>
              <div className="detail-card">
                <span>思考等级</span>
                <strong>{agent.thinkingLevel || '默认'}</strong>
              </div>
              <div className="detail-card">
                <span>绑定机器 ID</span>
                <strong style={monospace}>{agent.machineId || '暂无数据'}</strong>
              </div>
            </div>
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>System Prompt</span>
            </div>
            <pre className="daemon-command">{agent.systemPrompt?.trim() || '未配置 System Prompt，使用平台默认提示词。'}</pre>
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>能力与环境变量</span>
            </div>
            <div className="detail-grid">
              <div className="detail-card">
                <span>能力集 capabilitySet</span>
                <code style={monospace}>{jsonText(agent.capabilitySet)}</code>
              </div>
              <div className="detail-card">
                <span>环境变量 envVars</span>
                <code style={monospace}>{jsonText(agent.envVars)}</code>
              </div>
            </div>
          </section>
        </>
      )}

      {editing && agent ? (
        <AgentFormModal
          workspaceId={workspaceId || agent.workspaceId}
          machines={machines}
          agent={agent}
          onClose={() => setEditing(false)}
          onSaved={() => void load()}
        />
      ) : null}
    </AppShell>
  )
}
