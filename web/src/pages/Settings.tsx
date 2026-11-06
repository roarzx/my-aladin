import { useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { toast } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   系统设置
   对照 hercules-frontend 的工作区设置与模型代理配置。
   本地 Go 后端未提供设置接口，保存只提示，不伪造写入。
   ------------------------------------------------------------------ */

type ProxySource = 'none' | 'proxy' | 'custom'
type ProxyNode = 'cn' | 'us'

const PROXY_SOURCE_LABELS: Record<ProxySource, string> = {
  none: '未配置',
  proxy: '模型代理站点',
  custom: '自定义',
}

const PROXY_NODE_LABELS: Record<ProxyNode, string> = {
  cn: '国内 (CN)',
  us: '海外 (US)',
}

export default function Settings() {
  const [workspaceName, setWorkspaceName] = useState('')
  const [workspaceDescription, setWorkspaceDescription] = useState('')
  const [savingWorkspace, setSavingWorkspace] = useState(false)

  const [proxySource, setProxySource] = useState<ProxySource>('none')
  const [proxyNode, setProxyNode] = useState<ProxyNode>('cn')
  const [proxyKey, setProxyKey] = useState('')
  const [customKey, setCustomKey] = useState('')
  const [customBaseUrl, setCustomBaseUrl] = useState('')
  const [savingProxy, setSavingProxy] = useState(false)
  const [proxyNotice, setProxyNotice] = useState('')

  const saveWorkspace = (event: FormEvent) => {
    event.preventDefault()
    if (!workspaceName.trim()) {
      toast('请填写工作区名称')
      return
    }
    setSavingWorkspace(true)
    window.setTimeout(() => {
      setSavingWorkspace(false)
      toast('本地后端尚未提供该能力')
    }, 320)
  }

  const saveProxy = (event: FormEvent) => {
    event.preventDefault()
    if (proxySource === 'proxy') {
      if (!proxyKey.trim()) {
        setProxyNotice('请选择模型代理 key')
        return
      }
      if (!proxyNode) {
        setProxyNotice('请选择模型代理节点')
        return
      }
    }
    if (proxySource === 'custom') {
      if (!customBaseUrl.trim()) {
        setProxyNotice('请输入自定义 Base URL')
        return
      }
      if (!customKey.trim()) {
        setProxyNotice('请输入自定义 API Key')
        return
      }
    }
    setProxyNotice('')
    setSavingProxy(true)
    window.setTimeout(() => {
      setSavingProxy(false)
      toast('本地后端尚未提供该能力')
    }, 320)
  }

  return (
    <AppShell title="系统设置" breadcrumb="系统设置">
      <div className="settings-stack">
        <section className="panel workspace-settings-card">
          <div className="panel-head">
            <div>
              <h2>工作区设置</h2>
              <p>当前页面用于管理工作区基础信息与成员权限。工作区内的数据默认共享给全部成员。</p>
            </div>
          </div>

          <div className="settings-panel-body">
            <dl className="settings-summary-grid">
              <div className="settings-summary-item">
                <dt>当前工作区</dt>
                <dd>个人工作区</dd>
              </div>
              <div className="settings-summary-item">
                <dt>当前角色</dt>
                <dd>Owner</dd>
              </div>
              <div className="settings-summary-item">
                <dt>成员数</dt>
                <dd>1</dd>
              </div>
            </dl>
          </div>

          <form onSubmit={saveWorkspace} noValidate>
            <div className="workspace-settings-form">
              <label className="field">
                <span>工作区名称</span>
                <input
                  value={workspaceName}
                  maxLength={64}
                  placeholder="请输入工作区名称"
                  onChange={(event) => setWorkspaceName(event.target.value)}
                />
              </label>
              <label className="field wide">
                <span>工作区描述</span>
                <textarea
                  rows={4}
                  maxLength={255}
                  value={workspaceDescription}
                  placeholder="简单描述该工作区的用途"
                  onChange={(event) => setWorkspaceDescription(event.target.value)}
                />
              </label>
            </div>
            <div className="workspace-settings-actions">
              <button type="submit" className="button primary" disabled={savingWorkspace}>
                {savingWorkspace ? '保存中…' : '保存工作区'}
              </button>
            </div>
          </form>
        </section>

        <section className="panel workspace-settings-card">
          <div className="panel-head">
            <div>
              <h2>模型代理配置</h2>
              <p>选择模型代理来源，并填写对应的访问凭证。凭证保存后会加密存储。</p>
            </div>
          </div>

          <form onSubmit={saveProxy} noValidate>
            <div className="settings-panel-body">
              <div className="agent-proxy-config-panel">
                <label className="field">
                  <span>模型代理来源</span>
                  <select
                    value={proxySource}
                    onChange={(event) => {
                      setProxySource(event.target.value as ProxySource)
                      setProxyNotice('')
                    }}
                  >
                    {(Object.keys(PROXY_SOURCE_LABELS) as ProxySource[]).map((value) => (
                      <option key={value} value={value}>
                        {PROXY_SOURCE_LABELS[value]}
                      </option>
                    ))}
                  </select>
                </label>

                {proxySource === 'proxy' ? (
                  <>
                    <label className="field">
                      <span>模型代理节点</span>
                      <select
                        value={proxyNode}
                        onChange={(event) => setProxyNode(event.target.value as ProxyNode)}
                      >
                        {(Object.keys(PROXY_NODE_LABELS) as ProxyNode[]).map((value) => (
                          <option key={value} value={value}>
                            {PROXY_NODE_LABELS[value]}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="field wide">
                      <span>模型代理 key</span>
                      <select
                        value={proxyKey}
                        onChange={(event) => setProxyKey(event.target.value)}
                      >
                        <option value="">请选择模型代理 key</option>
                      </select>
                    </label>
                  </>
                ) : null}

                {proxySource === 'custom' ? (
                  <>
                    <label className="field wide">
                      <span>自定义 API Key</span>
                      <input
                        type="password"
                        value={customKey}
                        placeholder="请输入自定义 API Key（保存后加密存储）"
                        onChange={(event) => setCustomKey(event.target.value)}
                      />
                    </label>
                    <label className="field wide">
                      <span>自定义 Base URL</span>
                      <input
                        value={customBaseUrl}
                        placeholder="https://api.example.com/v1"
                        onChange={(event) => setCustomBaseUrl(event.target.value)}
                      />
                    </label>
                  </>
                ) : null}

                {proxyNotice ? (
                  <p className="notice warn wide" role="status">
                    {proxyNotice}
                  </p>
                ) : null}
              </div>
            </div>
            <div className="workspace-settings-actions">
              <button type="submit" className="button primary" disabled={savingProxy}>
                {savingProxy ? '保存中…' : '保存配置'}
              </button>
            </div>
          </form>
        </section>

        <section className="panel workspace-settings-card">
          <div className="panel-head">
            <div>
              <h2>账号信息</h2>
              <p>当前账号通过 Bearer 令牌访问本地后端，切换令牌后需要重新登录。</p>
            </div>
          </div>
          <div className="settings-panel-body">
            <div className="settings-account-grid">
              <div className="settings-account-item">
                <span className="label">账号</span>
                <span className="value">本地调试账户</span>
              </div>
              <div className="settings-account-item">
                <span className="label">角色</span>
                <span className="value">Owner</span>
              </div>
              <div className="settings-account-item">
                <span className="label">登录方式</span>
                <span className="value">Bearer 令牌</span>
              </div>
              <div className="settings-account-item">
                <span className="label">工作区</span>
                <span className="value">个人工作区</span>
              </div>
            </div>
          </div>
        </section>

        <section className="panel workspace-settings-card">
          <div className="panel-head">
            <div>
              <h2>外观说明</h2>
              <p>控制台当前仅提供浅色主题，界面配色与阿拉丁保持一致。</p>
            </div>
          </div>
          <div className="settings-panel-body">
            <div className="theme-preview">
              <span className="theme-swatch active">
                <i style={{ background: '#f8fafc' }} />
                浅色（当前）
              </span>
              <span className="theme-swatch">
                <i style={{ background: '#0f172a' }} />
                深色（暂未开放）
              </span>
            </div>
          </div>
        </section>
      </div>
    </AppShell>
  )
}
