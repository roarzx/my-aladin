import { useState } from 'react'

import AppShell from '../components/AppShell'
import { EmptyState, Panel } from '../components/ui'
import '../styles/pages-collab.css'

type ChannelFilter = 'all' | 'wecom' | 'feishu' | 'dingtalk'
type StatusFilter = 'all' | 'done' | 'pending'

const CHANNEL_FILTERS: Array<{ key: ChannelFilter; label: string }> = [
  { key: 'all', label: '全部渠道' },
  { key: 'wecom', label: '企微' },
  { key: 'feishu', label: '飞书' },
  { key: 'dingtalk', label: '钉钉' },
]

const STATUS_FILTERS: Array<{ key: StatusFilter; label: string }> = [
  { key: 'all', label: '全部状态' },
  { key: 'done', label: '已处理' },
  { key: 'pending', label: '待处理' },
]

export default function ChannelFeedback() {
  const [channel, setChannel] = useState<ChannelFilter>('all')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [search, setSearch] = useState('')

  const filtered = channel !== 'all' || status !== 'all' || search.trim().length > 0

  return (
    <AppShell title="渠道反馈列表" breadcrumb="渠道 / 渠道反馈">
      <Panel>
        <div className="toolbar channel-feedback-filter-bar">
          <div className="filters">
            {CHANNEL_FILTERS.map((item) => (
              <button
                key={item.key}
                type="button"
                className={`filter${channel === item.key ? ' active' : ''}`}
                onClick={() => setChannel(item.key)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <div className="filters">
            {STATUS_FILTERS.map((item) => (
              <button
                key={item.key}
                type="button"
                className={`filter${status === item.key ? ' active' : ''}`}
                onClick={() => setStatus(item.key)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <div className="search">
            <input
              type="search"
              value={search}
              placeholder="搜索用户、机器人、Agent 或问题"
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
        </div>

        {!filtered ? (
          <EmptyState
            title="暂无渠道反馈"
            description="本地后端尚未提供该能力。渠道用户提交反馈后，可在此查看并处理。"
          />
        ) : (
          <div className="table-scroll">
            <div className="data-table channel-feedback-table">
              <div className="data-table-row table-head">
                <span>反馈来源</span>
                <span>渠道</span>
                <span>类型</span>
                <span>内容摘要</span>
                <span>状态</span>
                <span>时间</span>
                <span>操作</span>
              </div>
              <div className="data-table-row">
                <span className="cell-muted">暂无符合筛选条件的反馈</span>
                <span>—</span>
                <span>—</span>
                <span className="cell-summary">—</span>
                <span>—</span>
                <span>—</span>
                <span className="cell-muted">—</span>
              </div>
            </div>
          </div>
        )}
      </Panel>
    </AppShell>
  )
}
