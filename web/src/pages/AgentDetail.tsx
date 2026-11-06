import { useMemo, useState } from 'react'

import AppShell from '../components/AppShell'
import { IconArrowLeft, IconRefresh } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link, useQueryParam } from '../router'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   Agent 详情 /agent-detail?id=…
   对照 hercules-frontend AgentDetailPage：基本信息 / 模型配置 /
   职责与约束规则 / 工具与 Skill 能力 / 绑定机器。
   本地后端尚未提供 Agent 接口，字段均显示暂无数据，不伪造数据。
   ------------------------------------------------------------------ */

interface RuleRow {
  id: number
  kind: 'constraint' | 'notice'
  text: string
}

let ruleSeed = 1

const RULE_KINDS: Array<{ label: string; value: RuleRow['kind'] }> = [
  { label: '约束', value: 'constraint' },
  { label: '提示', value: 'notice' },
]

const CAPABILITY_GROUPS = ['内置工具', 'Skills', 'MCP']

export default function AgentDetail() {
  const agentId = useQueryParam('id')
  const [duty, setDuty] = useState('')
  const [rules, setRules] = useState<RuleRow[]>([])
  const [machine, setMachine] = useState('')

  const previewRules = useMemo(() => rules.filter((rule) => rule.text.trim()), [rules])

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
          <button
            type="button"
            className="button ghost"
            onClick={() => toast('本地后端尚未提供该能力，无法刷新 Agent 信息。')}
          >
            <IconRefresh width={15} height={15} /> 刷新
          </button>
          <Link className="button subtle" to="/agents">
            <IconArrowLeft width={15} height={15} /> 返回列表
          </Link>
        </>
      }
    >
      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>
              Agent 详情 <StateTag tone="muted">状态未知</StateTag>
            </h2>
            <p>
              Agent ID：
              <code style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' }}>{agentId}</code>
            </p>
          </div>
          <div className="panel-actions">
            <button
              type="button"
              className="button ghost"
              onClick={() => toast('本地后端尚未提供该能力，无法编辑 Agent 配置。')}
            >
              编辑配置
            </button>
          </div>
        </div>

        <p className="resource-page-note">
          本地后端尚未提供该能力。接入 Agent 详情接口后，此处展示 Agent 的配置、职责规则与能力清单。
        </p>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>基本信息</span>
        </div>
        <div className="detail-grid">
          <div className="detail-card">
            <span>Agent 名称</span>
            <strong>暂无数据</strong>
          </div>
          <div className="detail-card">
            <span>Agent 类型</span>
            <strong>暂无数据</strong>
          </div>
          <div className="detail-card">
            <span>状态</span>
            <strong>暂无数据</strong>
          </div>
          <div className="detail-card">
            <span>所在机器</span>
            <strong>未绑定机器</strong>
          </div>
          <div className="detail-card">
            <span>工作目录</span>
            <strong>未上报</strong>
          </div>
          <div className="detail-card">
            <span>创建时间</span>
            <strong>暂无数据</strong>
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
            <strong>暂无数据</strong>
          </div>
          <div className="detail-card">
            <span>模型代理来源</span>
            <strong>未配置</strong>
          </div>
          <div className="detail-card">
            <span>模型代理节点</span>
            <strong>暂无数据</strong>
          </div>
        </div>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>职责与约束规则</span>
          <div className="resource-actions">
            <button
              type="button"
              className="button ghost small"
              onClick={() =>
                setRules((prev) => [...prev, { id: ruleSeed++, kind: 'constraint', text: '' }])
              }
            >
              添加规则
            </button>
            <button
              type="button"
              className="button subtle small"
              onClick={() => toast('本地后端尚未提供该能力，规则仅保存在当前页面。')}
            >
              保存规则
            </button>
          </div>
        </div>

        <label className="field" style={{ marginBottom: 10 }}>
          <span>职责</span>
          <textarea
            value={duty}
            rows={3}
            placeholder="描述该 Agent 的职责，例如：负责需求方案评审，输出评审意见。"
            onChange={(event) => setDuty(event.target.value)}
          />
        </label>

        {rules.length ? (
          <div className="rule-editor">
            {rules.map((rule) => (
              <div className="rule-editor-row" key={rule.id}>
                <select
                  value={rule.kind}
                  aria-label="规则类型"
                  onChange={(event) =>
                    setRules((prev) =>
                      prev.map((item) =>
                        item.id === rule.id ? { ...item, kind: event.target.value as RuleRow['kind'] } : item,
                      ),
                    )
                  }
                >
                  {RULE_KINDS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
                <input
                  value={rule.text}
                  placeholder="请输入规则内容"
                  aria-label="规则内容"
                  onChange={(event) =>
                    setRules((prev) =>
                      prev.map((item) => (item.id === rule.id ? { ...item, text: event.target.value } : item)),
                    )
                  }
                />
                <button
                  type="button"
                  className="resource-action danger"
                  onClick={() => setRules((prev) => prev.filter((item) => item.id !== rule.id))}
                >
                  删除
                </button>
              </div>
            ))}
          </div>
        ) : (
          <p className="rule-editor-empty">暂未配置约束规则。点击「添加规则」为 Agent 补充协作约束。</p>
        )}

        <div className="agent-rule-preview">
          <div className="agent-rule-preview-head">
            <strong>生效职责预览</strong>
            <span>{previewRules.length} 条规则</span>
          </div>
          {previewRules.length || duty.trim() ? (
            <ul className="agent-rule-preview-list">
              {duty.trim() ? <li>职责：{duty.trim()}</li> : null}
              {previewRules.map((rule) => (
                <li key={rule.id}>
                  {rule.kind === 'constraint' ? '约束' : '提示'}：{rule.text.trim()}
                </li>
              ))}
            </ul>
          ) : (
            <p className="agent-rule-preview-empty">填写职责与规则后，此处展示合并后的生效职责预览。</p>
          )}
        </div>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>工具与 Skill 能力</span>
        </div>
        {CAPABILITY_GROUPS.map((group) => (
          <div className="machine-readonly-section__head" key={group} style={{ marginTop: 6 }}>
            <span>{group}</span>
          </div>
        ))}
        <div className="empty-inline">
          <EmptyState
            title="暂无能力数据"
            description="本地后端尚未提供该能力。能力清单将在 Agent 启动时校验并上报。"
          />
        </div>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>绑定机器</span>
          <button
            type="button"
            className="button ghost small"
            onClick={() => toast('本地后端尚未提供该能力，无法修改绑定机器。')}
          >
            保存绑定
          </button>
        </div>
        <label className="field">
          <span>绑定机器</span>
          <select value={machine} disabled onChange={(event) => setMachine(event.target.value)}>
            <option value="">暂不绑定机器</option>
          </select>
          <small className="field-help">本地后端尚未提供该能力，暂无可选机器，接入后可在此切换绑定。</small>
        </label>
      </section>
    </AppShell>
  )
}
