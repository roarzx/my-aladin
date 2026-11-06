import { useCallback, useEffect, useState } from 'react'

import {
  fetchAgents,
  fetchMachines,
  fetchWorkspaces,
  issueMachineToken,
  type Agent,
  type Machine,
} from '../api/controlplane'
import { ApiError } from '../api/issues'
import AppShell from '../components/AppShell'
import { IconArrowLeft, IconPlus, IconRefresh } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link, useQueryParam } from '../router'
import { AgentFormModal } from './Agents'
import { daemonCommand } from './Machines'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   机器详情 /machine-detail?id=…
   后端暂无 GET /machines/{id}，这里按 workspace 拉取列表后按 id 定位。
   展示真实机器信息、关联 Agent，并支持重新签发一次性机器密钥。
   ------------------------------------------------------------------ */

const MACHINE_STATUS: Record<string, { label: string; tone: string }> = {
  online: { label: '在线', tone: 'done' },
  busy: { label: '忙碌', tone: 'running' },
  offline: { label: '离线', tone: 'muted' },
}

function machineStatusMeta(status: string): { label: string; tone: string } {
  return MACHINE_STATUS[status] ?? { label: status || '未知', tone: 'muted' }
}

function formatTime(value: string | null): string {
  if (!value) return '暂无数据'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

const monospace = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' }

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制到剪贴板')
  } catch {
    toast('复制失败，请手动选择复制')
  }
}

interface IssuedToken {
  token: string
  expiresAt: string
}

export default function MachineDetail() {
  const machineId = useQueryParam('id')
  const [machine, setMachine] = useState<Machine | null>(null)
  const [machines, setMachines] = useState<Machine[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [workspaceId, setWorkspaceId] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [issued, setIssued] = useState<IssuedToken | null>(null)
  const [issuing, setIssuing] = useState(false)
  const [createAgentOpen, setCreateAgentOpen] = useState(false)

  const load = useCallback(async () => {
    if (!machineId) {
      setLoading(false)
      return
    }
    setError('')
    try {
      const workspaces = await fetchWorkspaces()
      if (workspaces.length === 0) {
        setMachine(null)
        setMachines([])
        setAgents([])
        setLoading(false)
        return
      }
      const wid = workspaces[0].id
      setWorkspaceId(wid)
      const [machineList, agentList] = await Promise.all([fetchMachines(wid), fetchAgents(wid)])
      setMachines(machineList)
      setMachine(machineList.find((item) => item.id === machineId) ?? null)
      setAgents(agentList.filter((item) => item.machineId === machineId))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [machineId])

  useEffect(() => {
    void load()
  }, [load])

  const reissue = async () => {
    if (!machine) return
    setIssuing(true)
    setError('')
    try {
      const result = await issueMachineToken(machine.id)
      setIssued(result)
      toast('已重新签发机器密钥，请立即复制保存。')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setIssuing(false)
    }
  }

  if (!machineId) {
    return (
      <AppShell
        title="机器详情"
        breadcrumb="机器管理 / 机器详情"
        actions={
          <Link className="button ghost" to="/machines">
            <IconArrowLeft width={15} height={15} /> 返回机器列表
          </Link>
        }
      >
        <section className="panel">
          <div className="empty-inline">
            <EmptyState
              title="未选择机器"
              description="请从机器管理列表选择一台机器查看详情。"
              action={
                <Link className="button primary" to="/machines">
                  前往机器管理
                </Link>
              }
            />
          </div>
        </section>
      </AppShell>
    )
  }

  const meta = machine ? machineStatusMeta(machine.status) : { label: '未知', tone: 'muted' }

  return (
    <AppShell
      title="机器详情"
      breadcrumb={
        <>
          <Link to="/machines">机器管理</Link> / 机器详情
        </>
      }
      actions={
        <>
          <button type="button" className="button ghost" onClick={() => void load()}>
            <IconRefresh width={15} height={15} /> 刷新
          </button>
          <Link className="button subtle" to="/machines">
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
      ) : !machine ? (
        <section className="panel">
          <div className="empty-inline">
            <EmptyState
              title="未找到该机器"
              description="该机器可能已被删除，或不属于当前工作区。"
              action={
                <Link className="button primary" to="/machines">
                  返回机器管理
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
                  {machine.name} <StateTag tone={meta.tone}>{meta.label}</StateTag>
                </h2>
                <p>
                  机器 ID：<code style={monospace}>{machine.id}</code>
                </p>
              </div>
            </div>

            <div className="detail-grid">
              <div className="detail-card">
                <span>机器名称</span>
                <strong>{machine.name}</strong>
              </div>
              <div className="detail-card">
                <span>机器类型</span>
                <strong>{machine.kind || '暂无数据'}</strong>
              </div>
              <div className="detail-card">
                <span>连接状态</span>
                <strong>
                  <StateTag tone={meta.tone}>{meta.label}</StateTag>
                </strong>
              </div>
              <div className="detail-card">
                <span>CLI 版本</span>
                <strong>{machine.cliVersion || '暂无数据'}</strong>
              </div>
              <div className="detail-card">
                <span>最近心跳</span>
                <strong>{formatTime(machine.lastSeenAt)}</strong>
              </div>
              <div className="detail-card">
                <span>挂载 Agent 数</span>
                <strong>{agents.length}</strong>
              </div>
            </div>
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>关联 Agent</span>
              <button
                type="button"
                className="button ghost small"
                onClick={() => setCreateAgentOpen(true)}
              >
                <IconPlus width={13} height={13} /> 新增 Agent
              </button>
            </div>
            {agents.length ? (
              <div className="agent-board">
                {agents.map((agent) => (
                  <Link
                    key={agent.id}
                    className={`agent-card ${agent.enabled ? 'idle' : 'offline'}`}
                    to={`/agent-detail?id=${encodeURIComponent(agent.id)}`}
                  >
                    <div className="agent-card-head">
                      <strong>
                        {agent.name}
                        {agent.visibility === 'shared' ? <em className="shared-badge">共享</em> : null}
                      </strong>
                      <StateTag tone={agent.enabled ? 'done' : 'muted'}>
                        {agent.enabled ? '已启用' : '已停用'}
                      </StateTag>
                    </div>
                    <p>工作目录:{agent.workDir || '暂无数据'}</p>
                    <p>模型:{agent.model || '跟随 Agent 默认 Model'}</p>
                    <div className="machine-foot">
                      <span>{agent.visibility === 'shared' ? '共享' : '私有'}</span>
                      <span>详情</span>
                    </div>
                  </Link>
                ))}
              </div>
            ) : (
              <div className="empty-inline">
                <EmptyState
                  title="暂无关联 Agent"
                  description="创建一个绑定到该机器的 Agent 后，即可在创建任务时选择它执行步骤。"
                  action={
                    <button type="button" className="button ghost" onClick={() => setCreateAgentOpen(true)}>
                      新增 Agent
                    </button>
                  }
                />
              </div>
            )}
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>最近任务</span>
            </div>
            <div className="empty-inline">
              <EmptyState
                title="暂无任务记录"
                description="任务执行记录接口尚未提供，接入后此处展示该机器最近执行的任务。"
              />
            </div>
          </section>

          <section className="machine-readonly-section">
            <div className="machine-readonly-section__head">
              <span>CLI 接入</span>
              <div className="resource-actions">
                <button
                  type="button"
                  className="button subtle small"
                  disabled={issuing}
                  onClick={() => void reissue()}
                >
                  {issuing ? '签发中…' : '重新签发密钥'}
                </button>
              </div>
            </div>
            {issued ? (
              <div className="resource-modal-form">
                <p className="notice warn" role="status">
                  机器密钥仅展示一次，请立即复制保存；有效期至 {formatTime(issued.expiresAt)}。
                </p>
                <div className="field wide">
                  <span>机器密钥（ALADDIN_MACHINE_TOKEN）</span>
                  <div className="copy-row">
                    <code>{issued.token}</code>
                    <button
                      type="button"
                      className="button ghost small"
                      onClick={() => void copyText(issued.token)}
                    >
                      复制
                    </button>
                  </div>
                </div>
                <div className="field wide">
                  <span>daemon 启动命令</span>
                  <pre className="daemon-command">{daemonCommand(machine, issued.token)}</pre>
                  <button
                    type="button"
                    className="button ghost small"
                    onClick={() => void copyText(daemonCommand(machine, issued.token))}
                  >
                    复制启动命令
                  </button>
                </div>
                <p className="resource-page-note">
                  在目标机器上执行以上命令启动 daemon，上线后机器状态将变为「在线」。
                </p>
              </div>
            ) : (
              <p className="cli-command-footnote">
                注册时签发的一次性密钥不会再次展示；如未保存，可点击「重新签发密钥」获取新密钥与接入命令。
              </p>
            )}
          </section>
        </>
      )}

      {createAgentOpen && machine ? (
        <AgentFormModal
          workspaceId={workspaceId || machine.workspaceId}
          machines={machines}
          agent={null}
          defaultMachineId={machine.id}
          onClose={() => setCreateAgentOpen(false)}
          onSaved={() => void load()}
        />
      ) : null}
    </AppShell>
  )
}
