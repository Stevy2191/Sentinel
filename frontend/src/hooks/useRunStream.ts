import { useEffect, useState } from 'react'
import { apiBaseURL } from '@/services/api'
import type { ToolRun, ToolRunEvent, ToolRunStatus } from '@/types/netTools'

export interface RunStream {
  /** Every event received, by seq, without duplicates. */
  events: ToolRunEvent[]
  /** The latest status a frame reported, or null before one arrives. */
  status: ToolRunStatus | null
  /** The final run, once the `end` frame has arrived. */
  run: ToolRun | null
  error: string | null
}

const EMPTY: RunStream = { events: [], status: null, run: null, error: null }

/** Events arriving in a burst (a port scan) are gathered for this long
 *  before one re-render. */
const BATCH_MS = 100

interface State extends RunStream {
  id: string | null
}

function parse<T>(e: MessageEvent): T | null {
  try {
    return JSON.parse(String(e.data)) as T
  } catch {
    return null
  }
}

/**
 * Follows a run live over SSE (GET /tools/runs/:id/events).
 *
 * EventSource reconnects by itself and resends Last-Event-ID, and the server
 * replays from there; events are merged by seq, so a replay never duplicates.
 * The stream is closed on `end`, when runId changes (switching tool or run)
 * and on unmount. Frames from a closed source are ignored, so a late event
 * can never land on another run's view.
 */
export function useRunStream(runId: string | null): RunStream {
  const [state, setState] = useState<State>({ ...EMPTY, id: null })

  useEffect(() => {
    setState({ ...EMPTY, id: runId })
    if (!runId) return
    let open = true
    let timer = 0
    const bySeq = new Map<number, ToolRunEvent>()
    const flush = () => {
      if (timer) window.clearTimeout(timer)
      timer = 0
      const events = [...bySeq.values()].sort((a, b) => a.seq - b.seq)
      setState((s) => (s.id === runId ? { ...s, events } : s))
    }

    const src = new EventSource(`${apiBaseURL}/tools/runs/${encodeURIComponent(runId)}/events`, {
      withCredentials: true,
    })
    src.addEventListener('event', (e: MessageEvent) => {
      if (!open) return
      const ev = parse<ToolRunEvent>(e)
      if (!ev || bySeq.has(ev.seq)) return
      bySeq.set(ev.seq, ev)
      if (!timer) timer = window.setTimeout(flush, BATCH_MS)
    })
    src.addEventListener('status', (e: MessageEvent) => {
      if (!open) return
      const s = parse<{ status: ToolRunStatus }>(e)
      if (s) setState((st) => (st.id === runId ? { ...st, status: s.status } : st))
    })
    src.addEventListener('end', (e: MessageEvent) => {
      if (!open) return
      open = false
      src.close()
      flush()
      const run = parse<ToolRun>(e)
      if (run) setState((st) => (st.id === runId ? { ...st, run, status: run.status, error: null } : st))
    })
    src.onerror = () => {
      // While CONNECTING the browser is retrying on its own; CLOSED means it
      // gave up (the request was refused, e.g. 403 or 404).
      if (!open || src.readyState !== EventSource.CLOSED) return
      open = false
      flush()
      setState((st) => (st.id === runId ? { ...st, error: 'Live updates stopped' } : st))
    }

    return () => {
      open = false
      if (timer) window.clearTimeout(timer)
      src.close()
    }
  }, [runId])

  // The render between a runId change and the effect must not show the
  // previous run's events.
  if (state.id !== runId) return EMPTY
  return { events: state.events, status: state.status, run: state.run, error: state.error }
}
