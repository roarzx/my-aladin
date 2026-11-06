import { useState } from 'react'

import AppShell from '../components/AppShell'
import { IconArrowLeft, IconRefresh } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import { Link, useQueryParam } from '../router'
import '../styles/pages-resources.css'

/* ------------------------------------------------------------------
   机器详情 /machine-detail?id=…
   对照 hercules-frontend MachineDetailPage：只读信息卡 + 关联 Agent
   + 最近任务 + CLI 接入。本地后端未提供机器接口，字段均显示暂无数据。
   ------------------------------------------------------------------ */

const MACHINE_FIELDS = ['机器名称', '机器类型', '连接状态', '机器密钥', '创建时间', '最近心跳']

export default function MachineDetail() {
  const machineId = useQueryParam('id')
  const [showCommands, setShowCommands] = useState(false)

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
          <button
            type="button"
            className="button ghost"
            onClick={() => toast('本地后端尚未提供该能力，无法刷新机器信息。')}
          >
            <IconRefresh width={15} height={15} /> 刷新
          </button>
          <Link className="button subtle" to="/machines">
            <IconArrowLeft width={15} height={15} /> 返回列表
          </Link>
        </>
      }
    >
      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>
              机器详情 <StateTag tone="muted">连接状态未知</StateTag>
            </h2>
            <p>
              机器 ID：
              <code style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' }}>{machineId}</code>
            </p>
          </div>
        </div>

        <div className="detail-grid" style={{ marginBottom: 12 }}>
          {MACHINE_FIELDS.map((label) => (
            <div className="detail-card" key={label}>
              <span>{label}</span>
              <strong>暂无数据</strong>
            </div>
          ))}
        </div>

        <p className="resource-page-note">
          本地后端尚未提供该能力。接入机器详情接口后，此处展示机器名称、类型、连接状态、密钥与创建时间。
        </p>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>关联 Agent</span>
          <button
            type="button"
            className="button ghost small"
            onClick={() => toast('本地后端尚未提供该能力，无法在此新增 Agent。')}
          >
            新增 Agent
          </button>
        </div>
        <div className="empty-inline">
          <EmptyState
            title="暂无可见 Agent"
            description="本地后端尚未提供该能力。接入后此处展示该机器挂载的 Agent 实例。"
          />
        </div>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>最近任务</span>
        </div>
        <div className="empty-inline">
          <EmptyState
            title="暂无任务记录"
            description="本地后端尚未提供该能力。接入后此处展示该机器最近执行的任务。"
          />
        </div>
      </section>

      <section className="machine-readonly-section">
        <div className="machine-readonly-section__head">
          <span>CLI 接入</span>
          <div className="resource-actions">
            <button
              type="button"
              className="button ghost small"
              onClick={() => toast('本地后端尚未提供该能力，暂无 CLI 配置可复制。')}
            >
              复制 CLI 配置
            </button>
            <button
              type="button"
              className="button subtle small"
              onClick={() => setShowCommands((prev) => !prev)}
            >
              {showCommands ? '隐藏接入命令' : '显示接入命令'}
            </button>
          </div>
        </div>
        {showCommands ? (
          <p className="cli-command-footnote">
            本地后端尚未提供该能力。机器注册接口可用后，此处会显示一次性密钥与 CLI 接入命令。
          </p>
        ) : null}
      </section>
    </AppShell>
  )
}
