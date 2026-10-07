import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Loader2, RotateCcw, Square } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { cancelToolRun, useToolRun } from '@/hooks/useNetTools'
import { useRunStream } from '@/hooks/useRunStream'
import NoToolsAccess from '@/components/netTools/NoToolsAccess'
import RunResult from '@/components/netTools/RunResult'
import RunStatusLine from '@/components/netTools/RunStatusLine'
import type { ApiError } from '@/services/api'
import type { ToolRun, Vantage } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatDatetime } from '@/utils/formatters'
import { canUseNetTools, isFinalStatus, runTargetText, TOOL_LABEL, toolsQuery } from '@/utils/netTools'
import { mergeEvents, paramsText, runDuration } from '@/utils/netToolViews'

/** /tools/runs/:id: one run's result from its stored events, followed live
 *  while it is still going. */
export default function ToolRunDetail() {
  const { id } = useParams<{ id: string }>()
  const { currentUser } = useAuthContext()
  if (!canUseNetTools(currentUser)) return <NoToolsAccess />
  // Keyed by id: moving to another run starts clean and closes the old stream.
  return id ? <RunDetail key={id} id={id} /> : null
}

function BackLink() {
  return (
    <Link to="/tools" className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white">
      <ArrowLeft className="h-4 w-4" /> Back to Network Tools
    </Link>
  )
}

function RunDetail({ id }: { id: string }) {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const { detail, loading, error, reload } = useToolRun(id)
  const stored = detail?.run ?? null
  // Only an unfinished run is followed; a finished one is all in `detail`.
  // The stream replays from the start, and mergeEvents drops the overlap.
  const stream = useRunStream(stored && !isFinalStatus(stored.status) ? id : null)
  const [cancelled, setCancelled] = useState<ToolRun | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [cancelError, setCancelError] = useState<string | null>(null)
  // Once the stream ends (or gives up), read the stored run again: after a
  // cancel a few late events can be stored past `end`, and the stored run is
  // the truth.
  const ended = !!stream.run || !!stream.error
  useEffect(() => {
    if (ended) reload()
  }, [ended, reload])
  const events = useMemo(() => mergeEvents(detail?.events ?? [], stream.events), [detail, stream.events])

  if (loading && !detail) {
    return (
      <div className="flex items-center gap-2 p-6 text-sm text-slate-400">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading run…
      </div>
    )
  }
  if (!stored) {
    return (
      <div className="space-y-4">
        <BackLink />
        <div className={`rounded-lg border p-6 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {error ?? 'That run could not be found.'}
        </div>
      </div>
    )
  }

  const run: ToolRun = stream.run ?? cancelled ?? (stream.status ? { ...stored, status: stream.status } : stored)
  const live = !isFinalStatus(run.status)
  // Grant holders cancel their own runs; admins cancel any.
  const canCancel = live && (!!currentUser?.is_admin || run.user_id === currentUser?.user_id)
  const duration = runDuration(run)

  const cancel = async () => {
    setCancelling(true)
    setCancelError(null)
    try {
      setCancelled(await cancelToolRun(run.id))
    } catch (err) {
      setCancelError((err as ApiError).message || 'Could not cancel the run')
    } finally {
      setCancelling(false)
    }
  }

  const runAgain = () => {
    const vantage: Vantage =
      run.vantage_kind === 'agent' && run.agent_id ? { kind: 'agent', agent_id: run.agent_id } : { kind: 'sentinel' }
    navigate(`/tools${toolsQuery({ tool: run.tool, target: run.target, vantage, params: run.params })}`)
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <BackLink />
          <h1 className="vs-title mt-2 break-words text-3xl">
            {TOOL_LABEL[run.tool]} · {runTargetText(run)}
          </h1>
          <p className="mt-1 text-sm text-slate-400">
            From {run.vantage_name} · by {run.username} · {formatDatetime(run.created_at)}
            {duration ? ` · took ${duration}` : ''}
          </p>
          <p className="mt-1 text-xs text-slate-500">{paramsText(run.tool, run.params)}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {canCancel && (
            <button className="btn-secondary" disabled={cancelling} onClick={() => void cancel()}>
              <Square className="h-4 w-4" /> Cancel
            </button>
          )}
          <button className="btn-primary" onClick={runAgain}>
            <RotateCcw className="h-4 w-4" /> Run again
          </button>
        </div>
      </div>

      {cancelError && (
        <div className={`rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {cancelError}
        </div>
      )}

      <div className="card space-y-4 p-5">
        <RunStatusLine run={run} events={events} streamError={stream.error} />
        <RunResult run={run} events={events} />
      </div>
    </div>
  )
}
