import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { ExternalLink } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { cancelToolRun, createToolRun, useToolRuns, useVantages } from '@/hooks/useNetTools'
import { useRunStream } from '@/hooks/useRunStream'
import NoToolsAccess from '@/components/netTools/NoToolsAccess'
import RunHistory from '@/components/netTools/RunHistory'
import RunResult from '@/components/netTools/RunResult'
import RunStatusLine from '@/components/netTools/RunStatusLine'
import ToolForm from '@/components/netTools/ToolForm'
import type { ApiError } from '@/services/api'
import type { NetTool, ToolParams, ToolRun, ToolRunFilter } from '@/types/netTools'
import { colors } from '@/utils/colors'
import {
  canUseNetTools,
  cleanParams,
  DEFAULT_PARAMS,
  HISTORY_PAGE_SIZE,
  isFinalStatus,
  NET_TOOLS,
  parseToolsQuery,
  TOOL_LABEL,
  vantageFromKey,
  vantageKey,
} from '@/utils/netTools'

// The allowlist fences port checks only; ping, traceroute and DNS can reach
// any address that is not always blocked.
const EMPTY_ALLOWLIST =
  'No port-check targets are allowed yet. An admin adds subnets and hosts in Settings → Network tools'

/** /tools: run ping, traceroute, a DNS lookup or a port check and watch it
 *  live; the history of everyone's runs below. */
export default function NetworkTools() {
  const { currentUser } = useAuthContext()
  const { search } = useLocation()
  if (!canUseNetTools(currentUser)) return <NoToolsAccess />
  // Keyed by the query string: arriving from an entry point or "Run again"
  // starts a fresh form (and closes any open stream) rather than merging
  // into the one on screen.
  return <ToolsPage key={search} />
}

function initialParams(tool: NetTool | undefined, params: ToolParams | undefined): Record<NetTool, ToolParams> {
  const all = { ...DEFAULT_PARAMS }
  if (tool && params) all[tool] = { ...DEFAULT_PARAMS[tool], ...params }
  return all
}

function ToolsPage() {
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const [searchParams] = useSearchParams()
  const [prefill] = useState(() => parseToolsQuery(searchParams))

  const [tool, setTool] = useState<NetTool>(prefill.tool ?? 'ping')
  const [target, setTarget] = useState(prefill.target ?? '')
  const [vantage, setVantage] = useState(prefill.vantage ? vantageKey(prefill.vantage) : 'sentinel')
  const [params, setParams] = useState(() => initialParams(prefill.tool, prefill.params))
  const [current, setCurrent] = useState<ToolRun | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<{ text: string; limit: boolean } | null>(null)
  // Bumped by every new run and every tool switch, so a create or cancel
  // response that arrives after the user moved on is dropped.
  const attempt = useRef(0)
  const autoPicked = useRef(false)

  const { data: vantageData, error: vantageError } = useVantages()
  const stream = useRunStream(current?.id ?? null)
  const [filter, setFilter] = useState<ToolRunFilter>({ limit: HISTORY_PAGE_SIZE, offset: 0 })
  const history = useToolRuns(filter)
  const reloadHistory = history.reload

  // Nothing asked for and the Sentinel server cannot run tools: start on the
  // first agent that can, once.
  useEffect(() => {
    if (autoPicked.current || !vantageData || prefill.vantage) return
    autoPicked.current = true
    const list = vantageData.vantages ?? []
    if (list.find((v) => vantageKey(v) === 'sentinel')?.ready) return
    const firstReady = list.find((v) => v.ready)
    if (firstReady) setVantage(vantageKey(firstReady))
  }, [vantageData, prefill.vantage])

  // A finished run belongs in the history straight away.
  useEffect(() => {
    if (stream.run) reloadHistory()
  }, [stream.run, reloadHistory])

  // The run as this page knows it: the final row once the stream has ended,
  // else the created (or cancelled) row with the latest streamed status.
  const run: ToolRun | null = !current
    ? null
    : (stream.run ??
      (isFinalStatus(current.status) || !stream.status ? current : { ...current, status: stream.status }))
  // A stream that gave up while the run is unfinished must not lock the
  // form: stop treating the run as running (the history still shows it).
  const running = !!run && !isFinalStatus(run.status) && !stream.error

  const switchTool = (next: NetTool) => {
    if (next === tool) return
    attempt.current += 1
    setTool(next)
    setCurrent(null)
    setProblem(null)
    setBusy(false)
  }

  const start = async () => {
    attempt.current += 1
    const mine = attempt.current
    setBusy(true)
    setProblem(null)
    setCurrent(null)
    try {
      const created = await createToolRun({
        tool,
        vantage: vantageFromKey(vantage),
        target: target.trim(),
        params: cleanParams(tool, params[tool]),
      })
      if (mine !== attempt.current) return
      setCurrent(created)
      reloadHistory()
    } catch (err) {
      if (mine !== attempt.current) return
      const e = err as ApiError
      // Ruling 11: any 429 is a limit, whichever shape its body has.
      setProblem({ text: e.message || 'Could not start the run', limit: e.status === 429 })
    } finally {
      if (mine === attempt.current) setBusy(false)
    }
  }

  const cancel = async () => {
    if (!current) return
    const mine = attempt.current
    setBusy(true)
    try {
      const cancelled = await cancelToolRun(current.id)
      // The stream's end frame brings the same row; this covers a stream
      // that has already stopped.
      if (mine === attempt.current) setCurrent(cancelled)
    } catch (err) {
      if (mine === attempt.current) {
        setProblem({ text: (err as ApiError).message || 'Could not cancel the run', limit: false })
      }
    } finally {
      if (mine === attempt.current) setBusy(false)
    }
  }

  const problemTone = problem?.limit ? colors.warning : colors.error

  return (
    <div className="space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Network Tools</h1>
        <p className="text-sm text-slate-400">
          Ping, trace, look up names and check ports from Sentinel or a server agent.
        </p>
      </div>

      {tool === 'tcp' && vantageData?.allowlist_empty && (
        <div className={`rounded-lg border p-4 text-sm ${colors.warning.border} ${colors.warning.bg} ${colors.warning.text}`}>
          <p>{EMPTY_ALLOWLIST}</p>
          {isAdmin && (
            <Link to="/settings?tab=nettools" className="mt-1 inline-block underline">
              Open Settings → Network tools
            </Link>
          )}
        </div>
      )}

      {vantageError && (
        <div className={`rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {vantageError}
        </div>
      )}

      <div className="card space-y-5 p-5">
        <div className="flex flex-wrap gap-2" role="tablist" aria-label="Tool">
          {NET_TOOLS.map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={tool === t}
              onClick={() => switchTool(t)}
              className={`rounded-md px-4 py-2 text-sm font-medium ${
                tool === t ? 'bg-primary-600 text-white' : 'bg-white/5 text-slate-300'
              }`}
            >
              {TOOL_LABEL[t]}
            </button>
          ))}
        </div>

        <ToolForm
          tool={tool}
          target={target}
          onTarget={setTarget}
          vantage={vantage}
          onVantage={setVantage}
          vantages={vantageData?.vantages ?? []}
          params={params[tool]}
          onParams={(p) => setParams((all) => ({ ...all, [tool]: p }))}
          running={running}
          busy={busy}
          onRun={() => void start()}
          onCancel={() => void cancel()}
        />

        {problem && (
          <div className={`rounded-lg border p-3 text-sm ${problemTone.border} ${problemTone.bg} ${problemTone.text}`}>
            {problem.text}
          </div>
        )}
      </div>

      {run && (
        <div className="card space-y-4 p-5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <RunStatusLine run={run} events={stream.events} streamError={stream.error} />
            <Link
              to={`/tools/runs/${run.id}`}
              className="inline-flex items-center gap-1 text-sm text-primary-400 hover:underline"
            >
              Open run <ExternalLink className="h-3.5 w-3.5" />
            </Link>
          </div>
          <RunResult run={run} events={stream.events} />
        </div>
      )}

      <RunHistory
        runs={history.runs}
        total={history.total}
        loading={history.loading}
        error={history.error}
        filter={filter}
        onFilter={setFilter}
      />
    </div>
  )
}
