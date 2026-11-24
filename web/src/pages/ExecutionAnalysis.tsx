import { useCallback, useEffect, useState } from 'react'

import { fetchStats, type StatsOverview } from '../api/stats'
import AppShell from '../components/AppShell'
import { EmptyState, StateDot } from '../components/ui'
import { Link } from '../router'
import { formatMs, formatTokens } from './Dashboard'
import '../styles/pages-issues.css'

/**
 * Agent 执行分析（监控的第二块）。
 *
 * 只回答「谁在跑、跑得怎么样、慢在哪」：Agent 维度、工具耗时、失败分类、
 * token 归属。单次执行的完整轨迹在 Issue 驾驶舱的「记录」。
 */

const RANGES = [7, 30]

const FAILURE_LABELS: Record<string, string> = {
  network: '网络 / 超时',
  runtime: '运行环境',
  tool: '工具调用',
  verification: '验收',
  permission: '权限',
  dispatch: '派发',
}

const RUNTIME_LABELS: Record<string, string> = {
  claude_code: 'Claude Code',
  codex: 'Codex',
  kimi: 'Kimi',
}

function percent(ratio: number): string {
  return `${Math.round(ratio * 1000) / 10}%`
}

function formatTime(value: string): string {
  if (!value || value.startsWith('0001-')) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12: false })
}

function AgentTable({ overview }: { overview: StatsOverview }) {
  if (!overview.agents.length) {
    return (
      <EmptyState
        title="窗口内没有执行"
        description="Agent 跑过一轮之后，这里会按 Agent 汇总轮次、成功率与 token。"
      />
    )
  }
  return (
    <div className="recent-table" data-testid="agent-table">
      <div className="table-row table-head-row agent-row">
        <span>Agent</span>
        <span>运行时 / 模型</span>
        <span>轮次</span>
        <span>成功率</span>
        <span>P50 / P95</span>
        <span>工具</span>
        <span>Token（入 / 出）</span>
        <span>最近执行</span>
      </div>
      {overview.agents.map((agent) => (
        <Link
          className="table-row agent-row"
          key={agent.agentId}
          to={`/agent-detail?id=${encodeURIComponent(agent.agentId)}`}
        >
          <span className="cell-task">
            <strong>{agent.name || agent.agentId.slice(0, 8)}</strong>
            <span className="mono">{agent.agentId.slice(0, 8)}</span>
          </span>
          <span className="cell-muted">
            {RUNTIME_LABELS[agent.runtimeType] ?? (agent.runtimeType || '—')}
            <span className="cell-sub">{agent.model || '模型未配置'}</span>
          </span>
          <span>
            {agent.runs}
            <span className="cell-sub">
              {agent.succeeded} 完成 / {agent.failed} 失败
            </span>
          </span>
          <span className={agent.failed > 0 ? 'cell-danger' : ''}>
            {agent.finished ? percent(agent.successRate) : '—'}
          </span>
          <span className="cell-muted">
            {formatMs(agent.p50Ms)} / {formatMs(agent.p95Ms)}
          </span>
          <span className="cell-muted">{agent.toolCalls}</span>
          <span className="cell-muted">
            {formatTokens(agent.inputTokens)} / {formatTokens(agent.outputTokens)}
          </span>
          <span className="cell-muted">{formatTime(agent.lastRunAt)}</span>
        </Link>
      ))}
    </div>
  )
}

export default function ExecutionAnalysis() {
  const [stats, setStats] = useState<StatsOverview | null>(null)
  const [days, setDays] = useState(7)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setStats(await fetchStats(days))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [days])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <AppShell
      title="执行分析"
      breadcrumb="监控"
      actions={
        <div className="button-row">
          <div className="filters" role="group" aria-label="统计窗口">
            {RANGES.map((value) => (
              <button
                key={value}
                type="button"
                className={`filter${days === value ? ' active' : ''}`}
                onClick={() => setDays(value)}
              >
                {value} 天
              </button>
            ))}
          </div>
          <button type="button" className="button ghost" onClick={() => void load()} disabled={loading}>
            {loading ? '加载中…' : '刷新'}
          </button>
        </div>
      }
    >
      {error ? (
        <div className="notice danger" role="alert">
          加载失败：{error}
        </div>
      ) : null}

      {!stats && loading ? (
        <section className="panel">
          <div className="empty-inline">加载中…</div>
        </section>
      ) : null}

      {stats ? (
        <>
          <section className="panel">
            <div className="panel-head">
              <div>
                <h2>Agent 维度</h2>
                <p>
                  {stats.agents.length} 个 Agent · {stats.runs.total} 轮执行 ·{' '}
                  {stats.runs.retried} 轮为非首轮（含用户追加消息）
                </p>
              </div>
              <Link className="button subtle" to="/agents">
                管理 Agent
              </Link>
            </div>
            <AgentTable overview={stats} />
          </section>

          <div className="dashboard-grid">
            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>工具耗时</h2>
                  <p>按调用次数排序；分位基于每次调用的时长。</p>
                </div>
              </div>
              <div className="recent-table" data-testid="tool-table">
                <div className="table-row table-head-row tool-row">
                  <span>工具</span>
                  <span>调用</span>
                  <span>失败</span>
                  <span>P50</span>
                  <span>P95</span>
                </div>
                {stats.tools.length ? (
                  stats.tools.map((tool) => (
                    <div className="table-row tool-row" key={tool.name}>
                      <span className="cell-task">
                        <strong>{tool.name === 'unknown' ? '未知' : tool.name}</strong>
                      </span>
                      <span>{tool.count}</span>
                      <span className={tool.failed > 0 ? 'cell-danger' : 'cell-muted'}>{tool.failed}</span>
                      <span className="cell-muted">{formatMs(tool.p50Ms)}</span>
                      <span className="cell-muted">{formatMs(tool.p95Ms)}</span>
                    </div>
                  ))
                ) : (
                  <div className="empty-inline">窗口内没有工具调用</div>
                )}
              </div>
            </section>

            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>失败分类</h2>
                  <p>执行侧 error_class，定位问题在派发、网络还是工具。</p>
                </div>
              </div>
              {stats.failures.length ? (
                <div className="rank-list">
                  {stats.failures.map((item) => (
                    <div className="rank-item" key={item.name}>
                      <span className="rank-name">
                        <StateDot tone="failed" />
                        {FAILURE_LABELS[item.name] ?? item.name}
                      </span>
                      <span className="rank-count">{item.count}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="empty-inline">窗口内没有失败的轮次</div>
              )}
            </section>

            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>Token 按模型</h2>
                  <p>模型取执行时 Agent 配置的快照，未配置记为 unknown。</p>
                </div>
              </div>
              {stats.tokens.byModel.length ? (
                <div className="recent-table">
                  <div className="table-row table-head-row token-row">
                    <span>模型</span>
                    <span>输入</span>
                    <span>输出</span>
                  </div>
                  {stats.tokens.byModel.map((item) => (
                    <div className="table-row token-row" key={item.model}>
                      <span className="cell-task">
                        <strong>{item.model}</strong>
                      </span>
                      <span className="cell-muted">{formatTokens(item.input)}</span>
                      <span className="cell-muted">{formatTokens(item.output)}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="empty-inline">窗口内还没有 token 数据</div>
              )}
            </section>

            <section className="panel">
              <div className="panel-head">
                <div>
                  <h2>上下文与重试</h2>
                  <p>需要 daemon 侧埋点才能看到压缩行为。</p>
                </div>
              </div>
              <div className="rank-list">
                <div className="rank-item">
                  <span className="rank-name">非首轮执行</span>
                  <span className="rank-count">{stats.runs.retried}</span>
                </div>
                <div className="rank-item">
                  <span className="rank-name">平均每 Issue 轮次</span>
                  <span className="rank-count">
                    {stats.issues.total
                      ? (stats.runs.total / stats.issues.total).toFixed(1)
                      : '—'}
                  </span>
                </div>
              </div>
              <div className="panel-hint">
                上下文压缩、Memory 同步耗时尚未采集——ACP 事件里没有这些信息，
                需要 P1 在 daemon 侧加埋点。
              </div>
            </section>
          </div>
        </>
      ) : null}
    </AppShell>
  )
}
