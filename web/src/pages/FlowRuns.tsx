import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconRefresh, IconSearch } from '../components/icons'
import { EmptyState, Panel, StateTag, toast } from '../components/ui'
import { useQueryParam, useRouter } from '../router'
import '../styles/pages-resources-b.css'

/* ------------------------------------------------------------------
   流程执行 /resources/flow-runs
   对照 hercules-frontend 的 FlowRunsPage：状态胶囊 + 搜索 + 分页表格。
   本地后端尚未提供该能力，页面不伪造接口与数据。
   ------------------------------------------------------------------ */

type FlowRunStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'skipped'
  | 'timed_out'
  | 'pending'
  | 'not_started'

interface FlowRunRow {
  id: string
  displayId?: string
  flowId: string
  flowName: string
  flowVersion: string
  issueId: string
  issueTitle: string
  agentName: string
  environment: string
  status: FlowRunStatus
  createdAt: string
}

/** 本地后端尚未提供该能力，接口接入前列表恒为空。 */
const FLOW_RUNS: FlowRunRow[] = []

const FLOW_RUN_STATUS_LABEL: Record<string, string> = {
  queued: '待执行',
  running: '执行中',
  succeeded: '已完成',
  failed: '失败',
  cancelled: '已取消',
  skipped: '已跳过',
  timed_out: '已超时',
  pending: '待执行',
  not_started: '未开始',
}

function flowRunStatusLabel(status: string) {
  return FLOW_RUN_STATUS_LABEL[status] || status || '-'
}

function flowRunStatusClass(status: string) {
  if (status === 'succeeded' || status === 'skipped') return 'done'
  if (status === 'failed' || status === 'timed_out') return 'failed'
  if (status === 'cancelled' || status === 'not_started') return 'cancelled'
  if (status === 'running') return 'running'
  if (status === 'queued') return 'queued'
  return 'pending'
}

function flowRunEnvironmentLabel(environment: string) {
  if (environment === 'yktest') return '测试 · yktest'
  if (environment === 'ykprerelease') return '预发布 · ykprerelease'
  return environment || '-'
}

function formatFlowRunDisplayID(createdAt: string, id: string, displayId?: string) {
  if (displayId != null && displayId.trim()) return displayId.trim()
  const date = createdAt?.slice(0, 10).replace(/-/g, '') || '00000000'
  const short = id.replace(/-/g, '').slice(0, 8).toUpperCase()
  return `RUN-${date}-${short}`
}

const FLOW_RUN_FILTERS: Array<{ value: 'all' | FlowRunStatus; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'queued', label: '待执行' },
  { value: 'running', label: '执行中' },
  { value: 'succeeded', label: '已完成' },
  { value: 'failed', label: '失败' },
  { value: 'cancelled', label: '已取消' },
]

const PAGE_SIZES = [10, 20, 50]

export default function FlowRuns() {
  const router = useRouter()
  const focusId = useQueryParam('id')

  const [status, setStatus] = useState<'all' | FlowRunStatus>('all')
  const [keyword, setKeyword] = useState('')
  const [appliedKeyword, setAppliedKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const rows = useMemo(() => {
    const query = appliedKeyword.trim().toLowerCase()
    return FLOW_RUNS.filter((row) => {
      if (focusId && row.flowId !== focusId && row.id !== focusId) return false
      if (status !== 'all' && row.status !== status) return false
      if (!query) return true
      const displayId = formatFlowRunDisplayID(row.createdAt, row.id, row.displayId)
      return [displayId, row.id, row.issueTitle, row.issueId, row.flowName].some((value) =>
        value.toLowerCase().includes(query),
      )
    })
  }, [appliedKeyword, focusId, status])

  const total = rows.length
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const safePage = Math.min(page, pages)
  const pageRows = rows.slice((safePage - 1) * pageSize, safePage * pageSize)

  const applySearch = (event: FormEvent) => {
    event.preventDefault()
    setAppliedKeyword(keyword)
    setPage(1)
  }

  const resetPage = () => setPage(1)

  return (
    <AppShell title="流程执行" breadcrumb="资源管理 / 流程执行">
      <Panel title="全部执行" subtitle="跨 Issue 查看流程、目标 Agent、执行状态和运行时间。">
        <div className="rb-chips">
          {FLOW_RUN_FILTERS.map((item) => (
            <button
              key={item.value}
              type="button"
              className={`rb-chip ${status === item.value ? 'active' : ''}`}
              aria-pressed={status === item.value}
              onClick={() => {
                setStatus(item.value)
                resetPage()
              }}
            >
              {item.label}
            </button>
          ))}
        </div>

        {focusId ? (
          <div className="rb-filter-note">
            <span>当前仅显示流程 {focusId} 的执行记录。</span>
            <button
              type="button"
              className="rb-link-button"
              onClick={() => router.navigate('/resources/flow-runs')}
            >
              清除筛选
            </button>
          </div>
        ) : null}

        <div className="rb-toolbar">
          <form className="rb-toolbar-side" onSubmit={applySearch} role="search">
            <div className="rb-search">
              <IconSearch width={15} height={15} />
              <input
                value={keyword}
                aria-label="搜索流程执行"
                placeholder="搜索执行编号、Issue 或流程名称"
                onChange={(event) => setKeyword(event.target.value)}
              />
            </div>
            <button type="submit" className="button primary">
              搜索
            </button>
          </form>
          <button
            type="button"
            className="button ghost"
            onClick={() => toast('本地后端尚未提供该能力，无法刷新流程执行记录。')}
          >
            <IconRefresh width={15} height={15} /> 刷新
          </button>
        </div>

        {pageRows.length ? (
          <div className="rb-table cols-8">
            <div className="rb-table-head">
              <span>执行 ID</span>
              <span>Issue</span>
              <span>流程</span>
              <span>版本</span>
              <span>目标 Agent</span>
              <span>环境</span>
              <span>状态</span>
              <span>创建时间</span>
            </div>
            {pageRows.map((row) => {
              const displayId = formatFlowRunDisplayID(row.createdAt, row.id, row.displayId)
              return (
                <div className="rb-row" key={row.id}>
                  <div className="rb-cell" title={displayId}>
                    {displayId}
                  </div>
                  <div className="rb-cell rb-cell-name" title={row.issueId ? `${row.issueTitle} (${row.issueId})` : row.issueTitle}>
                    <strong>{row.issueTitle || row.issueId || '-'}</strong>
                    {row.issueId ? <span className="rb-muted">{row.issueId}</span> : null}
                  </div>
                  <div className="rb-cell" title={row.flowName}>
                    {row.flowName || '-'}
                  </div>
                  <div className="rb-cell rb-muted">{row.flowVersion ? `v${row.flowVersion}` : '-'}</div>
                  <div className="rb-cell rb-muted">{row.agentName || '-'}</div>
                  <div className="rb-cell rb-muted">{flowRunEnvironmentLabel(row.environment)}</div>
                  <div className="rb-cell">
                    <StateTag tone={flowRunStatusClass(row.status)}>{flowRunStatusLabel(row.status)}</StateTag>
                  </div>
                  <div className="rb-cell rb-muted">{row.createdAt || '-'}</div>
                </div>
              )
            })}
          </div>
        ) : (
          <div className="rb-empty">
            <EmptyState
              title="暂无流程执行记录"
              description="本地后端尚未提供该能力。从 Issue 会话运行已启用流程后，记录会出现在这里。"
            />
          </div>
        )}

        {total > 0 ? (
          <div className="rb-pagination">
            <span>
              共 {total} 条，第 {safePage} / {pages} 页
            </span>
            <div className="rb-pagination-actions">
              <button
                type="button"
                className="rb-page-button"
                disabled={safePage <= 1}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
              >
                上一页
              </button>
              <button
                type="button"
                className="rb-page-button"
                disabled={safePage >= pages}
                onClick={() => setPage((value) => Math.min(pages, value + 1))}
              >
                下一页
              </button>
              <select
                className="rb-page-size"
                aria-label="每页条数"
                value={pageSize}
                onChange={(event) => {
                  setPageSize(Number(event.target.value))
                  setPage(1)
                }}
              >
                {PAGE_SIZES.map((size) => (
                  <option key={size} value={size}>
                    {size} 条/页
                  </option>
                ))}
              </select>
            </div>
          </div>
        ) : null}
      </Panel>
    </AppShell>
  )
}
