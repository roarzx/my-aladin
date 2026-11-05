import { useCallback, useEffect, useRef, useState } from 'react'

import { fetchEvents, type EventPage, type StreamEvent } from '../api/issues'

export interface UseEventStreamOptions {
  pollIntervalMs?: number
  /** 可注入的拉取函数（测试用）。 */
  fetcher?: (taskId: string, after: number) => Promise<EventPage>
  maxBackoffMs?: number
}

export interface EventStreamState {
  events: StreamEvent[]
  cursor: number
  connected: boolean
  error: Error | null
}

/**
 * 带游标的事件流（D14）：
 * - 游标规则是 `seq > cursor`，容忍 seq 有洞；
 * - 同一 eventId 重复到达时丢弃；
 * - 拉取失败时保留游标、指数退避重连，恢复后不丢事件、不重复渲染。
 */
export function useEventStream(
  taskId: string,
  afterSeq = 0,
  options: UseEventStreamOptions = {},
): EventStreamState {
  const { pollIntervalMs = 1000, fetcher = fetchEvents, maxBackoffMs = 10000 } = options

  const [events, setEvents] = useState<StreamEvent[]>([])
  const [cursor, setCursor] = useState(afterSeq)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<Error | null>(null)

  const cursorRef = useRef(afterSeq)
  const seenRef = useRef<Set<string>>(new Set())

  // reset 允许外部游标变化时重新开始（测试与页面刷新语义）
  const reset = useCallback((next: number) => {
    cursorRef.current = next
    seenRef.current = new Set()
    setEvents([])
    setCursor(next)
  }, [])

  useEffect(() => {
    reset(afterSeq)
  }, [taskId, afterSeq, reset])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let failures = 0

    const tick = async () => {
      try {
        const page = await fetcher(taskId, cursorRef.current)
        if (cancelled) return
        failures = 0
        setConnected(true)
        setError(null)

        const additions = page.events.filter(
          (e) => e.seq > cursorRef.current && !seenRef.current.has(e.eventId),
        )
        if (additions.length > 0) {
          additions.forEach((e) => seenRef.current.add(e.eventId))
          setEvents((prev) => [...prev, ...additions])
        }
        const next = Math.max(cursorRef.current, page.nextSeq)
        if (next !== cursorRef.current) {
          cursorRef.current = next
          setCursor(next)
        }
        timer = setTimeout(tick, pollIntervalMs)
      } catch (err) {
        if (cancelled) return
        failures += 1
        setConnected(false)
        setError(err instanceof Error ? err : new Error(String(err)))
        const backoff = Math.min(maxBackoffMs, 500 * 2 ** (failures - 1))
        timer = setTimeout(tick, backoff)
      }
    }

    setConnected(false)
    void tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [taskId, fetcher, pollIntervalMs, maxBackoffMs])

  return { events, cursor, connected, error }
}
