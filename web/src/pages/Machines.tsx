import { useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconRefresh } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   机器管理 /machines
   对照 hercules-frontend MachinesPage：panel-head + machine-grid。
   本地 Go 后端仅提供 Issue 接口，这里不伪造数据与接口。
   ------------------------------------------------------------------ */

interface MachineCardRow {
  id: string
  name: string
  hostname: string
  ip: string
  os: string
  status: 'online' | 'busy' | 'offline'
  cloud: boolean
  shared: boolean
  lastSeen: string
  cliVersion: string
}

/** 本地后端尚未提供机器能力，接口接入前列表恒为空。 */
const MACHINES: MachineCardRow[] = []

const MACHINE_STATUS: Record<MachineCardRow['status'], { label: string; tone: string }> = {
  online: { label: '在线', tone: 'done' },
  busy: { label: '忙碌', tone: 'running' },
  offline: { label: '离线', tone: 'muted' },
}

type MachineType = 'on_premise' | 'cloud'

export function CreateMachineModal({
  defaultCloud = false,
  onClose,
}: {
  defaultCloud?: boolean
  onClose: () => void
}) {
  const [machineType, setMachineType] = useState<MachineType>(defaultCloud ? 'cloud' : 'on_premise')
  const [name, setName] = useState('')
  const [notice, setNotice] = useState('')

  const cloud = machineType === 'cloud'
  const title = cloud ? '创建云机器' : '注册机器'

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) {
      setNotice('请输入机器名称。')
      return
    }
    if (name.trim().length > 64) {
      setNotice('机器名称不能超过64个字符。')
      return
    }
    setNotice('本地后端尚未提供该能力，注册不会写入数据库。')
    toast(`本地后端尚未提供该能力，未能${cloud ? '创建' : '注册'}机器：${name.trim()}。`)
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label={title} style={{ width: 'min(680px, 100%)' }}>
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>{title}</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="resource-modal-form">
              <div className="field wide">
                <span id="machine-type-label">机器类型</span>
                <div className="resource-radio-cards" role="radiogroup" aria-labelledby="machine-type-label">
                  <label className={`resource-radio-card ${machineType === 'on_premise' ? 'active' : ''}`}>
                    <input
                      type="radio"
                      name="machine-type"
                      checked={machineType === 'on_premise'}
                      onChange={() => setMachineType('on_premise')}
                    />
                    <span>
                      本地 / 自有机器
                      <small>在自有主机上安装 CLI 并注册，使用机器密钥接入。</small>
                    </span>
                  </label>
                  <label className={`resource-radio-card ${machineType === 'cloud' ? 'active' : ''}`}>
                    <input
                      type="radio"
                      name="machine-type"
                      checked={machineType === 'cloud'}
                      onChange={() => setMachineType('cloud')}
                    />
                    <span>
                      云端机器
                      <small>由平台按需创建云实例，任务运行时自动启动。</small>
                    </span>
                  </label>
                </div>
              </div>

              <label className="field wide">
                <span>机器名称</span>
                <input
                  value={name}
                  maxLength={64}
                  placeholder="请输入机器名称，如 build-01"
                  onChange={(event) => setName(event.target.value)}
                />
              </label>

              <p className="resource-page-note">
                {cloud
                  ? '创建成功后任务需要时会自动调度云端实例启动。'
                  : '注册成功后会生成一次性机器密钥，将其配置到目标机器的 CLI 环境中。'}
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
            <button type="submit" className="button primary">
              {cloud ? '创建' : '注册'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function MachineCard({ machine }: { machine: MachineCardRow }) {
  const meta = MACHINE_STATUS[machine.status]
  return (
    <Link className={`machine-card ${machine.status}`} to={`/machine-detail?id=${encodeURIComponent(machine.id)}`}>
      <div className="machine-top">
        <strong>
          {machine.name}
          {machine.cloud ? <em className="cloud-badge">云机器</em> : null}
          {machine.shared ? <em className="shared-badge">共享</em> : null}
        </strong>
        <StateTag tone={meta.tone}>{meta.label}</StateTag>
      </div>
      <div className="machine-meta">
        <span>{machine.ip || '暂无 IP'}</span>
        <span>{machine.os || 'unknown'}</span>
        <span>CLI {machine.cliVersion || '暂无数据'}</span>
      </div>
      <p>宿主机：{machine.hostname || '暂无数据'}</p>
      <div className="machine-foot">
        <span>最近心跳：{machine.lastSeen || '暂无数据'}</span>
        <span>查看机器详情</span>
      </div>
    </Link>
  )
}

export default function Machines() {
  const [createOpen, setCreateOpen] = useState(false)
  const [groupFilter, setGroupFilter] = useState<'all' | 'mine'>('all')

  const machines = groupFilter === 'mine' ? MACHINES.filter((item) => !item.shared) : MACHINES

  return (
    <AppShell
      title="机器与 CLI 接入"
      breadcrumb="机器管理"
      actions={
        <button type="button" className="button ghost" onClick={() => toast('本地后端尚未提供该能力，无法刷新机器列表。')}>
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
            <button type="button" className="button ghost" onClick={() => setGroupFilter(groupFilter === 'all' ? 'mine' : 'all')}>
              {groupFilter === 'all' ? '只看我的开发机' : '查看全部机器'}
            </button>
            <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
              <IconPlus width={15} height={15} /> 注册机器
            </button>
          </div>
        </div>

        <div className="machine-group-head">
          我的开发机
          <span>共 {machines.length} 台</span>
        </div>

        {machines.length ? (
          <div className="machine-grid">
            {machines.map((machine) => (
              <MachineCard key={machine.id} machine={machine} />
            ))}
          </div>
        ) : (
          <div className="empty-inline">
            <EmptyState
              title="当前用户暂无机器"
              description="本地后端尚未提供该能力。页面结构按阿拉丁设计还原，接入接口后即可展示机器心跳与挂载 Agent。"
              action={
                <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                  注册机器
                </button>
              }
            />
          </div>
        )}
      </section>

      {createOpen ? <CreateMachineModal onClose={() => setCreateOpen(false)} /> : null}
    </AppShell>
  )
}
