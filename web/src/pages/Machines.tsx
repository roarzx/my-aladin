import { useCallback, useEffect, useState, type FormEvent } from 'react'

import {
  createMachine,
  fetchMachines,
  fetchWorkspaces,
  type Machine,
} from '../api/controlplane'
import { ApiError } from '../api/issues'
import AppShell from '../components/AppShell'
import { IconPlus, IconRefresh } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   机器管理 /machines
   真实数据：GET /workspaces → GET /machines?workspaceId=；
   注册弹窗调用 POST /machines，展示一次性 token 与 daemon 启动命令。
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

/** 机器注册成功后的 daemon 启动命令（密钥只返回一次明文）。 */
export function daemonCommand(machine: Machine, token: string): string {
  return [
    'make build',
    '',
    `ALADDIN_MACHINE_TOKEN=${token} \\`,
    'ALADDIN_ACP_URL=ws://127.0.0.1:8080/v1/acp \\',
    'ALADDIN_API_URL=http://127.0.0.1:8080 \\',
    './bin/daemon --config config/config.yaml --profile default \\',
    `  --machine-id ${machine.id}`,
  ].join('\n')
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制到剪贴板')
  } catch {
    toast('复制失败，请手动选择复制')
  }
}

interface RegisteredMachine {
  machine: Machine
  token: string
  expiresAt: string
}

export function CreateMachineModal({
  workspaceId,
  onClose,
  onRegistered,
}: {
  workspaceId: string
  onClose: () => void
  onRegistered: () => void
}) {
  const [name, setName] = useState('')
  const [kind, setKind] = useState('local')
  const [notice, setNotice] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [registered, setRegistered] = useState<RegisteredMachine | null>(null)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setNotice('请输入机器名称。')
      return
    }
    if (trimmed.length > 64) {
      setNotice('机器名称不能超过64个字符。')
      return
    }
    setSubmitting(true)
    setNotice('')
    try {
      const result = await createMachine({ workspaceId, name: trimmed, kind })
      setRegistered(result)
      onRegistered()
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
      <div className="modal" role="dialog" aria-modal="true" aria-label="注册机器" style={{ width: 'min(680px, 100%)' }}>
        {registered ? (
          <>
            <div className="modal-head">
              <h3>机器注册成功</h3>
              <button type="button" className="button ghost small" onClick={onClose}>
                关闭
              </button>
            </div>
            <div className="modal-body">
              <div className="resource-modal-form">
                <p className="notice warn" role="status">
                  机器密钥仅展示一次，请立即复制保存；有效期至 {formatTime(registered.expiresAt)}。
                </p>
                <div className="field wide">
                  <span>机器 ID</span>
                  <div className="copy-row">
                    <code>{registered.machine.id}</code>
                    <button
                      type="button"
                      className="button ghost small"
                      onClick={() => void copyText(registered.machine.id)}
                    >
                      复制
                    </button>
                  </div>
                </div>
                <div className="field wide">
                  <span>机器密钥（ALADDIN_MACHINE_TOKEN）</span>
                  <div className="copy-row">
                    <code>{registered.token}</code>
                    <button
                      type="button"
                      className="button ghost small"
                      onClick={() => void copyText(registered.token)}
                    >
                      复制
                    </button>
                  </div>
                </div>
                <div className="field wide">
                  <span>daemon 启动命令</span>
                  <pre className="daemon-command">{daemonCommand(registered.machine, registered.token)}</pre>
                  <button
                    type="button"
                    className="button ghost small"
                    onClick={() => void copyText(daemonCommand(registered.machine, registered.token))}
                  >
                    复制启动命令
                  </button>
                </div>
                <p className="resource-page-note">
                  在目标机器上执行以上命令启动 daemon，上线后本页将显示「在线」。
                </p>
              </div>
            </div>
            <div className="modal-foot">
              <button type="button" className="button primary" onClick={onClose}>
                完成
              </button>
            </div>
          </>
        ) : (
          <form onSubmit={(event) => void submit(event)}>
            <div className="modal-head">
              <h3>注册机器</h3>
              <button type="button" className="button ghost small" onClick={onClose}>
                关闭
              </button>
            </div>
            <div className="modal-body">
              <div className="resource-modal-form">
                <label className="field wide">
                  <span>机器名称</span>
                  <input
                    value={name}
                    maxLength={64}
                    placeholder="请输入机器名称，如 build-01"
                    onChange={(event) => setName(event.target.value)}
                  />
                </label>
                <label className="field wide">
                  <span>机器类型</span>
                  <select value={kind} onChange={(event) => setKind(event.target.value)}>
                    <option value="local">local（本地/自有机器）</option>
                    <option value="cloud">cloud（云端机器）</option>
                  </select>
                </label>
                <p className="resource-page-note">
                  注册成功后会生成一次性机器密钥，将其配置到目标机器的 daemon 环境中。
                </p>
                {notice ? (
                  <p className="notice warn" role="status">
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
                {submitting ? '注册中…' : '注册'}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}

function MachineCard({ machine }: { machine: Machine }) {
  const meta = machineStatusMeta(machine.status)
  return (
    <Link className={`machine-card ${machine.status}`} to={`/machine-detail?id=${encodeURIComponent(machine.id)}`}>
      <div className="machine-top">
        <strong>{machine.name}</strong>
        <StateTag tone={meta.tone}>{meta.label}</StateTag>
      </div>
      <div className="machine-meta">
        <span>{machine.kind || 'unknown'}</span>
        <span>CLI {machine.cliVersion || '暂无数据'}</span>
      </div>
      <div className="machine-foot">
        <span>最近心跳：{formatTime(machine.lastSeenAt)}</span>
        <span>查看机器详情</span>
      </div>
    </Link>
  )
}

export default function Machines() {
  const [workspaceId, setWorkspaceId] = useState('')
  const [machines, setMachines] = useState<Machine[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      let wid = workspaceId
      if (!wid) {
        const workspaces = await fetchWorkspaces()
        if (workspaces.length === 0) {
          setMachines([])
          setLoading(false)
          return
        }
        wid = workspaces[0].id
        setWorkspaceId(wid)
      }
      setMachines(await fetchMachines(wid))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspaceId])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <AppShell
      title="机器与 CLI 接入"
      breadcrumb="机器管理"
      actions={
        <button type="button" className="button ghost" onClick={() => void load()}>
          <IconRefresh width={15} height={15} /> 刷新
        </button>
      }
    >
      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>机器管理</h2>
            <p>注册 CLI 执行环境，查看心跳与挂载 Agent。</p>
          </div>
          <div className="panel-actions">
            <button type="button" className="button primary" onClick={() => setCreateOpen(true)} disabled={!workspaceId}>
              <IconPlus width={15} height={15} /> 注册机器
            </button>
          </div>
        </div>

        <div className="machine-group-head">
          我的开发机
          <span>共 {machines.length} 台</span>
        </div>

        {error ? (
          <p className="notice danger" role="alert">
            加载失败:{error}
          </p>
        ) : null}

        {loading ? (
          <div className="empty-inline">加载中…</div>
        ) : machines.length ? (
          <div className="machine-grid">
            {machines.map((machine) => (
              <MachineCard key={machine.id} machine={machine} />
            ))}
          </div>
        ) : (
          <div className="empty-inline">
            <EmptyState
              title="当前用户暂无机器"
              description="注册一台机器并启动 daemon 后，即可创建 Agent 并执行任务。"
              action={
                <button type="button" className="button ghost" onClick={() => setCreateOpen(true)} disabled={!workspaceId}>
                  注册机器
                </button>
              }
            />
          </div>
        )}
      </section>

      {createOpen ? (
        <CreateMachineModal
          workspaceId={workspaceId}
          onClose={() => setCreateOpen(false)}
          onRegistered={() => void load()}
        />
      ) : null}
    </AppShell>
  )
}
