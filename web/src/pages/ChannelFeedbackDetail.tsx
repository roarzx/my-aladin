import { useState } from 'react'

import AppShell from '../components/AppShell'
import { EmptyState, Panel, StateTag } from '../components/ui'
import { Link, useQueryParam } from '../router'
import '../styles/pages-collab.css'

export default function ChannelFeedbackDetail() {
  const id = useQueryParam('id')
  const [targetOpen, setTargetOpen] = useState(true)

  if (!id) {
    return (
      <AppShell title="反馈处理详情" breadcrumb="渠道 / 渠道反馈 / 反馈详情">
        <Panel>
          <EmptyState
            title="未选择反馈"
            description="请从渠道反馈列表中选择一条反馈查看详情。"
            action={
              <Link className="button primary" to="/channel-feedback">
                返回列表
              </Link>
            }
          />
        </Panel>
      </AppShell>
    )
  }

  return (
    <AppShell title="反馈处理详情" breadcrumb="渠道 / 渠道反馈 / 反馈详情">
      <Panel>
        <div className="channel-feedback-detail-head">
          <div>
            <h2>反馈处理详情</h2>
            <div className="feedback-id">反馈编号：{id}</div>
          </div>
          <Link className="button ghost" to="/channel-feedback">
            返回列表
          </Link>
        </div>

        <div className="notice warn">本地后端尚未提供渠道反馈详情接口，当前仅还原页面结构。</div>

        <div className="channel-feedback-detail-grid">
          <div className="stack">
            <section className="channel-feedback-section">
              <div className="channel-feedback-section-title">反馈内容</div>
              <div className="cell-muted">暂无反馈内容</div>
            </section>

            <section className="channel-feedback-section">
              <button
                type="button"
                className="channel-feedback-target-collapse"
                aria-expanded={targetOpen}
                onClick={() => setTargetOpen((open) => !open)}
              >
                <span>反馈对象</span>
                <span className="hint">{targetOpen ? '收起' : '展开'}</span>
              </button>
              {targetOpen ? (
                <div className="conversation-inspector-meta">
                  <span className="label">用户</span>
                  <strong>—</strong>
                  <span className="label">机器人</span>
                  <strong>—</strong>
                  <span className="label">Agent</span>
                  <strong>—</strong>
                  <span className="label">问题</span>
                  <strong>—</strong>
                </div>
              ) : null}
            </section>

            <section className="channel-feedback-section">
              <div className="channel-feedback-section-title">对话记录</div>
              <div className="cell-muted">暂无对话记录</div>
            </section>
          </div>

          <div className="stack">
            <section className="channel-feedback-side-card">
              <div className="channel-feedback-section-title">处理信息</div>
              <div className="conversation-inspector-meta">
                <span className="label">指派人</span>
                <strong>未指派</strong>
                <span className="label">处理状态</span>
                <span>
                  <StateTag tone="pending">待处理</StateTag>
                </span>
                <span className="label">反馈时间</span>
                <strong>—</strong>
                <span className="label">反馈类型</span>
                <strong>—</strong>
              </div>
              <div className="row-between">
                <button type="button" className="button small" disabled>
                  标记已处理
                </button>
                <button type="button" className="button ghost small" disabled>
                  标记待处理
                </button>
              </div>
            </section>

            <section className="channel-feedback-side-card">
              <div className="channel-feedback-section-title">处理时间线</div>
              <div className="channel-feedback-timeline">
                <div className="channel-feedback-timeline-item">
                  <span className="dot" />
                  <span>暂无处理记录</span>
                </div>
              </div>
            </section>
          </div>
        </div>
      </Panel>
    </AppShell>
  )
}
