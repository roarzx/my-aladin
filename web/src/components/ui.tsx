import { useEffect, useState, type ReactNode } from 'react'

import { Link } from '../router'

export type TagTone =
  | 'done'
  | 'failed'
  | 'running'
  | 'queued'
  | 'pending'
  | 'cancelled'
  | 'review'
  | 'paused'
  | 'draft'
  | 'saved'
  | 'muted'

export function StateTag({ tone, children }: { tone: TagTone | string; children: ReactNode }) {
  return <span className={`state-tag ${tone}`}>{children}</span>
}

export function StateDot({ tone }: { tone: TagTone | string }) {
  return <i className={`state-dot ${tone}`} aria-hidden />
}

interface PanelProps {
  title?: ReactNode
  subtitle?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
  id?: string
  compact?: boolean
}

export function Panel({ title, subtitle, actions, children, className, id, compact }: PanelProps) {
  const head = title || subtitle || actions
  return (
    <section className={['panel', className].filter(Boolean).join(' ')} id={id}>
      {head ? (
        <div className={compact ? 'panel-head compact' : 'panel-head'}>
          <div>
            {title ? <h2>{title}</h2> : null}
            {subtitle ? <p>{subtitle}</p> : null}
          </div>
          {actions ? <div className="panel-actions">{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </section>
  )
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="empty-state">
      <strong>{title}</strong>
      {description ? <p>{description}</p> : null}
      {action}
    </div>
  )
}

export function MetricCard({
  label,
  value,
  foot,
  to,
  urgent,
}: {
  label: string
  value: ReactNode
  foot?: ReactNode
  to?: string
  urgent?: boolean
}) {
  const className = ['metric-card', urgent ? 'urgent' : ''].filter(Boolean).join(' ')
  const inner = (
    <>
      <div className="metric-label">{label}</div>
      <div className="metric-value">{value}</div>
      {foot ? <div className="metric-foot">{foot}</div> : null}
    </>
  )
  return to ? (
    <Link className={className} to={to}>
      {inner}
    </Link>
  ) : (
    <div className={className}>{inner}</div>
  )
}

/* ---------- Toast ---------- */

type ToastListener = (message: string) => void
const toastListeners = new Set<ToastListener>()

export function toast(message: string) {
  toastListeners.forEach((listener) => listener(message))
}

export function ToastHost() {
  const [message, setMessage] = useState('')
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const listener: ToastListener = (next) => {
      setMessage(next)
      setVisible(true)
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => setVisible(false), 2200)
    }
    toastListeners.add(listener)
    return () => {
      toastListeners.delete(listener)
      if (timer) clearTimeout(timer)
    }
  }, [])

  return (
    <div className={`toast ${visible ? 'show' : ''}`} role="status" aria-live="polite">
      {message}
    </div>
  )
}
