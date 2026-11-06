import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconRefresh, IconSearch } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   Agent 管理 /agents
   对照 hercules-frontend AgentsPage：panel-head 工具栏 + agent-board
   卡片列表；创建弹层按「新增 Agent」原字段还原。
   本地后端尚未提供 Agent 接口，列表恒为空，不伪造数据。
   ------------------------------------------------------------------ */

interface AgentRow {
  id: string
  name: string
  type: string
  machineName: string
  model: string
  status: 'idle' | 'running' | 'offline'
  lastActive: string
  shared: boolean
}

/** 本地后端尚未提供 Agent 能力，接口接入前列表恒为空。 */
const AGENTS: AgentRow[] = []

const AGENT_STATUS: Record<AgentRow['status'], { label: string; tone: string }> = {
  idle: { label: '空闲', tone: 'done' },
  running: { label: '运行中', tone: 'running' },
  offline: { label: '离线', tone: 'muted' },
}

type AgentType = 'claude_code' | 'codex' | 'kimi' | 'custom'

const AGENT_TYPES: Array<{ label: string; value: AgentType }> = [
  { label: 'Claude Code', value: 'claude_code' },
  { label: 'Codex', value: 'codex' },
  { label: 'Kimi Code', value: 'kimi' },
  { label: '自定义', value: 'custom' },
]

type ProxySource = '' | 'proxy' | 'custom'

const PROXY_SOURCES: Array<{ label: string; value: Exclude<ProxySource, ''> }> = [
  { label: '模型代理站点', value: 'proxy' },
  { label: '自定义', value: 'custom' },
]

type ProxyNode = '' | 'cn' | 'us'

const PROXY_NODES: Array<{ label: string; value: Exclude<ProxyNode, ''> }> = [
  { label: '国内 (CN)', value: 'cn' },
  { label: '海外 (US)', value: 'us' },
]

function AgentCard({ agent }: { agent: AgentRow }) {
  const meta = AGENT_STATUS[agent.status]
  return (
    <Link className={`agent-card ${agent.status}`} to={`/agent-detail?id=${encodeURIComponent(agent.id)}`}>
      <div className="agent-card-head">
        <strong>
          {agent.name || agent.type}
          {agent.shared ? <em className="shared-badge">共享</em> : null}
        </strong>
        <StateTag tone={meta.tone}>{meta.label}</StateTag>
      </div>
      <p>绑定机器：{agent.machineName || '未绑定机器'}</p>
      <p>模型：{agent.model || '跟随 Agent 默认 Model'}</p>
      <div className="machine-foot">
        <span>最近活跃：{agent.lastActive || '暂无数据'}</span>
        <span>详情</span>
      </div>
    </Link>
  )
}

export function CreateAgentModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [agentType, setAgentType] = useState<AgentType>('claude_code')
  const [model, setModel] = useState('')
  const [proxySource, setProxySource] = useState<ProxySource>('')
  const [proxyNode, setProxyNode] = useState<ProxyNode>('')
  const [proxyKey, setProxyKey] = useState('')
  const [customKey, setCustomKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [notice, setNotice] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setNotice('请输入 Agent 名称。')
      return
    }
    if (trimmed.length > 64) {
      setNotice('Agent 名称不能超过64个字符。')
      return
    }
    if (!agentType) {
      setNotice('请选择 Agent 类型。')
      return
    }
    if (proxySource && !proxyNode) {
      setNotice('请选择模型代理节点。')
      return
    }
    if (proxySource === 'proxy' && !proxyKey.trim()) {
      setNotice('请输入模型代理 key。')
      return
    }
    if (proxySource === 'custom' && !customKey.trim()) {
      setNotice('请输入自定义 API Key。')
      return
    }
    if (proxySource === 'custom' && !baseUrl.trim()) {
      setNotice('请输入自定义 Base URL。')
      return
    }
    setNotice('本地后端尚未提供该能力，创建 Agent 不会写入数据库。')
    toast(`本地后端尚未提供该能力，未能创建 Agent：${trimmed}。`)
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label="新增 Agent" style={{ width: 'min(760px, 100%)' }}>
        <form onSubmit={submit}>
          <div className="modal-head">
            <h3>新增 Agent</h3>
            <button type="button" className="button ghost small" onClick={onClose}>
              关闭
            </button>
          </div>
          <div className="modal-body">
            <div className="resource-modal-form">
              <p className="resource-page-note">
                创建 Agent 配置时可绑定到指定机器，系统将同时创建 Agent 实例。模型代理配置用于让 Agent 通过模型代理站点访问大模型。
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
                    <span>Agent 类型</span>
                    <select value={agentType} onChange={(event) => setAgentType(event.target.value as AgentType)}>
                      {AGENT_TYPES.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </label>

                  <label className="field">
                    <span>使用范围</span>
                    <div className="agent-usage-scope-options" role="radiogroup" aria-label="使用范围">
                      <label>
                        <input type="radio" name="usage-scope" defaultChecked /> 私有
                      </label>
                      <label>
                        <input type="radio" name="usage-scope" /> 共享
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
                </div>
              </section>

              <section className="resource-section">
                <h4 className="resource-section-title">模型代理配置</h4>
                <div className="agent-proxy-config-panel">
                  <div className="agent-proxy-config-row">
                    <label className="field">
                      <span>模型代理来源</span>
                      <select
                        value={proxySource}
                        onChange={(event) => {
                          setProxySource(event.target.value as ProxySource)
                          setProxyNode('')
                        }}
                      >
                        <option value="">未配置</option>
                        {PROXY_SOURCES.map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </select>
                    </label>

                    {proxySource ? (
                      <label className="field">
                        <span>模型代理节点</span>
                        <select value={proxyNode} onChange={(event) => setProxyNode(event.target.value as ProxyNode)}>
                          <option value="">请选择模型代理节点</option>
                          {PROXY_NODES.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </label>
                    ) : null}
                  </div>

                  {proxySource === 'proxy' ? (
                    <label className="field">
                      <span>模型代理 key</span>
                      <input
                        value={proxyKey}
                        placeholder="请输入模型代理 key"
                        onChange={(event) => setProxyKey(event.target.value)}
                      />
                      <small className="field-help">选择节点后填写该节点下可用的 key。</small>
                    </label>
                  ) : null}

                  {proxySource === 'custom' ? (
                    <div className="agent-proxy-config-row">
                      <label className="field">
                        <span>自定义 API Key</span>
                        <input
                          type="password"
                          value={customKey}
                          placeholder="请输入自定义 API Key"
                          onChange={(event) => setCustomKey(event.target.value)}
                        />
                      </label>
                      <label className="field">
                        <span>自定义 Base URL</span>
                        <input
                          value={baseUrl}
                          placeholder="请输入自定义 Base URL"
                          onChange={(event) => setBaseUrl(event.target.value)}
                        />
                      </label>
                    </div>
                  ) : null}

                  <small className="field-help">
                    来源选「未配置」时使用平台默认模型网关；该配置仅在本机保存前展示，本地后端尚未提供写入能力。
                  </small>
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
            <button type="button" className="button subtle" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="button primary">
              确认创建
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default function Agents() {
  const [keyword, setKeyword] = useState('')
  const [createOpen, setCreateOpen] = useState(false)

  const filtered = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    if (!query) return AGENTS
    return AGENTS.filter((agent) =>
      [agent.name, agent.type, agent.machineName, agent.model].join(' ').toLowerCase().includes(query),
    )
  }, [keyword])

  return (
    <AppShell
      title="Agent 运行控制"
      breadcrumb="Agent 管理"
      actions={
        <button type="button" className="button ghost" onClick={() => toast('本地后端尚未提供该能力，无法刷新 Agent 列表。')}>
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
            <button type="button" className="button primary" onClick={() => setCreateOpen(true)}>
              <IconPlus width={15} height={15} /> 创建 Agent
            </button>
          </div>
        </div>

        {filtered.length ? (
          <div className="agent-board">
            {filtered.map((agent) => (
              <AgentCard key={agent.id} agent={agent} />
            ))}
          </div>
        ) : (
          <div className="empty-inline">
            <EmptyState
              title={keyword.trim() ? '没有匹配的 Agent' : '当前用户暂无 Agent 或未绑定配置'}
              description={
                keyword.trim()
                  ? '没有找到符合搜索条件的 Agent，请调整关键词后重试。'
                  : '本地后端尚未提供该能力。页面结构按阿拉丁设计还原，接入 Agent 接口后即可展示配置与运行状态。'
              }
              action={
                keyword.trim() ? (
                  <button type="button" className="button ghost" onClick={() => setKeyword('')}>
                    清空搜索
                  </button>
                ) : (
                  <button type="button" className="button ghost" onClick={() => setCreateOpen(true)}>
                    创建 Agent
                  </button>
                )
              }
            />
          </div>
        )}
      </section>

      {createOpen ? <CreateAgentModal onClose={() => setCreateOpen(false)} /> : null}
    </AppShell>
  )
}
