import { useCallback, useEffect, useState, type FormEvent } from 'react'

import {
  createIssue,
  createRepository,
  fetchAgents,
  fetchRepositories,
  fetchWorkspaces,
  type Agent,
  type Repository,
} from '../api/controlplane'
import { ApiError } from '../api/issues'
import { saveTaskDraft } from '../api/taskDraft'
import AppShell from '../components/AppShell'
import { IconArrowLeft, IconPlus, IconRefresh } from '../components/icons'
import { EmptyState, Panel, StateTag, toast } from '../components/ui'
import { Link, useRouter } from '../router'
import '../styles/pages-issues.css'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   创建任务 /task/create
   真实数据：GET /workspaces → GET /agents?workspaceId= 与 /repositories?workspaceId=；
   提交 POST /workspaces/{wid}/issues，成功后跳转 /task/detail?id=<taskId>。
   需求描述只暂存在浏览器侧，作为驾驶舱提交计划时的默认正文。
   ------------------------------------------------------------------ */

function formatRepo(repo: Repository): string {
  return `${repo.url}（${repo.defaultBranch || 'main'}）`
}

export default function CreateTask() {
  const { navigate } = useRouter()
  const [step, setStep] = useState<1 | 2>(1)
  const [workspaceId, setWorkspaceId] = useState('')
  const [agents, setAgents] = useState<Agent[]>([])
  const [repositories, setRepositories] = useState<Repository[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [agentId, setAgentId] = useState('')
  const [repositoryId, setRepositoryId] = useState('')
  const [notice, setNotice] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const [repoOpen, setRepoOpen] = useState(false)
  const [repoUrl, setRepoUrl] = useState('')
  const [repoBranch, setRepoBranch] = useState('main')
  const [repoNotice, setRepoNotice] = useState('')
  const [repoSaving, setRepoSaving] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const workspaces = await fetchWorkspaces()
      if (workspaces.length === 0) {
        setWorkspaceId('')
        setAgents([])
        setRepositories([])
        return
      }
      const wid = workspaces[0].id
      setWorkspaceId(wid)
      const [agentList, repoList] = await Promise.all([fetchAgents(wid), fetchRepositories(wid)])
      setAgents(agentList)
      setRepositories(repoList)
      setAgentId((prev) => prev || (agentList.find((item) => item.enabled)?.id ?? ''))
      setRepositoryId((prev) => prev || (repoList[0]?.id ?? ''))
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const enabledAgents = agents.filter((agent) => agent.enabled)

  const goNext = () => {
    if (!name.trim()) {
      setNotice('请输入任务名称')
      return
    }
    setNotice('')
    setStep(2)
  }

  const addRepository = async (event: FormEvent) => {
    event.preventDefault()
    const url = repoUrl.trim()
    if (!url) {
      setRepoNotice('请输入仓库地址。')
      return
    }
    if (!/^(https?:\/\/|git@|ssh:\/\/)/i.test(url)) {
      setRepoNotice('仓库地址格式不正确，例如 https://github.com/owner/repo。')
      return
    }
    if (!workspaceId) {
      setRepoNotice('暂无可用工作区，无法添加仓库。')
      return
    }
    setRepoSaving(true)
    setRepoNotice('')
    try {
      const repo = await createRepository({
        workspaceId,
        url,
        defaultBranch: repoBranch.trim() || 'main',
      })
      setRepositories((prev) => [...prev.filter((item) => item.id !== repo.id), repo])
      setRepositoryId(repo.id)
      setRepoUrl('')
      setRepoBranch('main')
      setRepoOpen(false)
      toast(`已添加仓库：${repo.url}`)
    } catch (err) {
      setRepoNotice(err instanceof ApiError ? err.message : String(err))
    } finally {
      setRepoSaving(false)
    }
  }

  const submit = async () => {
    if (!name.trim()) {
      setNotice('请输入任务名称')
      return
    }
    if (!workspaceId) {
      setNotice('暂无可用工作区，请先加入工作区。')
      return
    }
    if (!agentId) {
      setNotice('请选择执行 Agent。')
      return
    }
    setSubmitting(true)
    setNotice('')
    try {
      const created = await createIssue(workspaceId, {
        name: name.trim(),
        agentIds: [agentId],
        plannerAgentId: agentId,
        repositories: repositoryId ? [repositoryId] : [],
      })
      saveTaskDraft(created.id, description)
      toast('任务已创建，正在进入驾驶舱…')
      navigate(`/task/detail?id=${encodeURIComponent(created.id)}`)
    } catch (err) {
      setNotice(err instanceof ApiError ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  const selectedAgent = agents.find((agent) => agent.id === agentId)

  return (
    <AppShell
      title="创建开发任务"
      breadcrumb="我的 Issue / 创建任务"
      actions={
        <>
          <button type="button" className="button ghost" onClick={() => void load()}>
            <IconRefresh width={15} height={15} /> 刷新
          </button>
          <Link className="button subtle" to="/task/list">
            <IconArrowLeft width={15} height={15} /> 返回 Issue 列表
          </Link>
        </>
      }
    >
      <div className="create-steps">
        <div className={step === 1 ? 'create-step active' : 'create-step'}>
          <span className="step-index">1</span>
          <span>任务信息</span>
        </div>
        <div className={step === 2 ? 'create-step active' : 'create-step'}>
          <span className="step-index">2</span>
          <span>执行 Agent 与仓库</span>
        </div>
      </div>

      {loadError ? (
        <p className="notice danger" role="alert">
          加载失败:{loadError}
        </p>
      ) : null}

      {loading ? (
        <Panel title="加载中" subtitle="正在读取工作区、Agent 与仓库…">
          <div className="empty-inline">加载中…</div>
        </Panel>
      ) : step === 1 ? (
        <Panel title="任务信息" subtitle="填写任务名称与需求描述，创建后进入驾驶舱提交计划。">
          <div className="form-grid">
            <label className="field wide">
              <span>任务名称</span>
              <input
                value={name}
                maxLength={128}
                onChange={(event) => {
                  setName(event.target.value)
                  if (notice) setNotice('')
                }}
                placeholder="例如：修复登录页验证码无法刷新"
                aria-invalid={Boolean(notice)}
              />
              <small>用于在 Issue 列表与驾驶舱中识别任务。</small>
            </label>

            <label className="field wide">
              <span>需求描述</span>
              <textarea
                rows={8}
                className="system-prompt-input"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="补充背景、验收标准与相关链接。该内容会作为驾驶舱提交计划时的默认正文。"
              />
              <small>仅保存在当前浏览器，用于预填计划正文，不作为任务字段提交。</small>
            </label>
          </div>

          {notice ? (
            <p className="notice danger" role="alert">
              {notice}
            </p>
          ) : null}

          <div className="form-actions">
            <button
              type="button"
              className="button ghost"
              onClick={() => navigate('/task/list')}
            >
              取消
            </button>
            <button type="button" className="button primary" onClick={goNext}>
              下一步：选择 Agent 与仓库
            </button>
          </div>
        </Panel>
      ) : (
        <Panel
          title="执行 Agent 与仓库"
          subtitle="选择执行 Agent 与代码仓库后创建任务；daemon 在 Agent 绑定的机器上执行步骤并创建 PR。"
        >
          <div className="stack">
            <div className="create-section-title">
              选择执行 Agent <span className="required">* 必选</span>
              <span className="create-note">（在 Agent 绑定的机器上执行）</span>
            </div>

            {enabledAgents.length ? (
              <div className="form-grid">
                <label className="field wide">
                  <span>执行 Agent</span>
                  <select value={agentId} onChange={(event) => setAgentId(event.target.value)}>
                    <option value="">请选择 Agent</option>
                    {enabledAgents.map((agent) => (
                      <option key={agent.id} value={agent.id}>
                        {agent.name} · {agent.workDir || '未配置工作目录'}
                      </option>
                    ))}
                  </select>
                  <small>
                    {selectedAgent
                      ? `规划与执行均由该 Agent 承担（plannerAgentId 同为 ${selectedAgent.name}）。`
                      : '创建任务后可在驾驶舱提交计划并启动执行。'}
                  </small>
                </label>
              </div>
            ) : (
              <EmptyState
                title="暂无可用 Agent"
                description="请先在 Agent 管理创建并启用一个绑定在线机器的 Agent。"
                action={
                  <Link className="button ghost" to="/agents">
                    创建 Agent 配置
                  </Link>
                }
              />
            )}

            <div className="create-section-title">
              选择代码仓库
              <span className="create-note">（v1 仅支持 1 个仓库，用于 clone、提交与创建 PR）</span>
            </div>

            <div className="form-grid">
              <label className="field wide">
                <span>仓库</span>
                <select
                  value={repositoryId}
                  onChange={(event) => setRepositoryId(event.target.value)}
                >
                  <option value="">{repositories.length ? '不使用仓库（不创建 PR）' : '暂无仓库'}</option>
                  {repositories.map((repo) => (
                    <option key={repo.id} value={repo.id}>
                      {formatRepo(repo)}
                    </option>
                  ))}
                </select>
                <small>未选择仓库时，Agent 直接在其工作目录执行，不会提交代码或创建 PR。</small>
              </label>
            </div>

            {repoOpen ? (
              <form className="resource-section" onSubmit={(event) => void addRepository(event)}>
                <h4 className="resource-section-title">新增仓库</h4>
                <div className="form-grid">
                  <label className="field wide">
                    <span>仓库地址</span>
                    <input
                      value={repoUrl}
                      placeholder="https://github.com/owner/repo"
                      onChange={(event) => {
                        setRepoUrl(event.target.value)
                        if (repoNotice) setRepoNotice('')
                      }}
                    />
                  </label>
                  <label className="field">
                    <span>默认分支</span>
                    <input
                      value={repoBranch}
                      placeholder="main"
                      onChange={(event) => setRepoBranch(event.target.value)}
                    />
                    <small>创建 PR 时作为 base 分支。</small>
                  </label>
                </div>
                {repoNotice ? (
                  <p className="notice danger" role="alert">
                    {repoNotice}
                  </p>
                ) : null}
                <div className="form-actions">
                  <button
                    type="button"
                    className="button ghost"
                    onClick={() => {
                      setRepoOpen(false)
                      setRepoNotice('')
                    }}
                  >
                    取消
                  </button>
                  <button type="submit" className="button primary" disabled={repoSaving}>
                    {repoSaving ? '添加中…' : '添加仓库'}
                  </button>
                </div>
              </form>
            ) : (
              <div>
                <button type="button" className="button ghost" onClick={() => setRepoOpen(true)}>
                  <IconPlus width={15} height={15} /> 新增仓库
                </button>
              </div>
            )}

            {selectedAgent ? (
              <p className="resource-page-note">
                执行 Agent：<strong>{selectedAgent.name}</strong>{' '}
                <StateTag tone="done">已启用</StateTag>
              </p>
            ) : null}
          </div>

          {notice ? (
            <p className="notice danger" role="alert">
              {notice}
            </p>
          ) : null}

          <div className="form-actions">
            <button type="button" className="button ghost" onClick={() => setStep(1)}>
              上一步
            </button>
            <button
              type="button"
              className="button primary"
              disabled={submitting || !enabledAgents.length}
              onClick={() => void submit()}
            >
              {submitting ? '创建中…' : '创建任务并进入驾驶舱'}
            </button>
          </div>
        </Panel>
      )}
    </AppShell>
  )
}
