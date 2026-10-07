import { Link, useNavigate } from 'react-router-dom'
import type { NetTool, ToolRun, ToolRunFilter, ToolRunStatus } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatDatetime } from '@/utils/formatters'
import {
  HISTORY_PAGE_SIZE,
  NET_TOOLS,
  runResultLine,
  runTargetText,
  STATUS_PILL,
  statusLabel,
  TOOL_LABEL,
  TOOL_RUN_STATUSES,
} from '@/utils/netTools'

interface Props {
  runs: ToolRun[]
  total: number
  loading: boolean
  error: string | null
  filter: ToolRunFilter
  onFilter: (f: ToolRunFilter) => void
}

/** Everyone's runs, newest first (a team troubleshooting tool), filtered by
 *  tool, mine or all, and status. A row opens the run. */
export default function RunHistory({ runs, total, loading, error, filter, onFilter }: Props) {
  const navigate = useNavigate()
  const offset = filter.offset ?? 0
  // Any filter change starts again from the first page.
  const set = (patch: Partial<ToolRunFilter>) => onFilter({ ...filter, ...patch, offset: 0 })

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">History</h2>
        <div className="flex flex-wrap gap-2">
          <select
            className="rd-select"
            aria-label="Filter by tool"
            value={filter.tool ?? ''}
            onChange={(e) => set({ tool: (e.target.value || undefined) as NetTool | undefined })}
          >
            <option value="">All tools</option>
            {NET_TOOLS.map((t) => (
              <option key={t} value={t}>
                {TOOL_LABEL[t]}
              </option>
            ))}
          </select>
          <select
            className="rd-select"
            aria-label="Whose runs"
            value={filter.mine ? 'mine' : 'all'}
            onChange={(e) => set({ mine: e.target.value === 'mine' || undefined })}
          >
            <option value="all">Everyone&apos;s runs</option>
            <option value="mine">My runs</option>
          </select>
          <select
            className="rd-select"
            aria-label="Filter by status"
            value={filter.status ?? ''}
            onChange={(e) => set({ status: (e.target.value || undefined) as ToolRunStatus | undefined })}
          >
            <option value="">Any status</option>
            {TOOL_RUN_STATUSES.map((s) => (
              <option key={s} value={s}>
                {statusLabel(s)}
              </option>
            ))}
          </select>
        </div>
      </div>

      {error && (
        <div className={`rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                <th className="px-4 py-3 font-medium">Time</th>
                <th className="px-4 py-3 font-medium">User</th>
                <th className="px-4 py-3 font-medium">Tool</th>
                <th className="px-4 py-3 font-medium">Target</th>
                <th className="px-4 py-3 font-medium">From</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Result</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {runs.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                    {loading ? 'Loading…' : 'No runs yet.'}
                  </td>
                </tr>
              ) : (
                runs.map((r) => (
                  <tr
                    key={r.id}
                    className="cursor-pointer transition hover:bg-white/5"
                    onClick={() => navigate(`/tools/runs/${r.id}`)}
                  >
                    <td className="whitespace-nowrap px-4 py-3">
                      <Link
                        to={`/tools/runs/${r.id}`}
                        className="text-slate-300 hover:underline"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {formatDatetime(r.created_at)}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-slate-300">{r.username}</td>
                    <td className="px-4 py-3 text-slate-300">{TOOL_LABEL[r.tool]}</td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-300">{runTargetText(r)}</td>
                    <td className="px-4 py-3 text-slate-400">{r.vantage_name}</td>
                    <td className="px-4 py-3">
                      <span className={`rd-pill ${STATUS_PILL[r.status]}`}>{statusLabel(r.status)}</span>
                    </td>
                    <td className="px-4 py-3 text-slate-300" title={r.error ?? undefined}>
                      {runResultLine(r)}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {total > HISTORY_PAGE_SIZE && (
        <div className="flex items-center justify-between text-sm text-slate-500">
          <span>
            {offset + 1}–{Math.min(offset + runs.length, total)} of {total}
          </span>
          <div className="flex gap-2">
            <button
              className="btn-secondary !py-1"
              disabled={offset === 0}
              onClick={() => onFilter({ ...filter, offset: Math.max(0, offset - HISTORY_PAGE_SIZE) })}
            >
              Previous
            </button>
            <button
              className="btn-secondary !py-1"
              disabled={offset + HISTORY_PAGE_SIZE >= total}
              onClick={() => onFilter({ ...filter, offset: offset + HISTORY_PAGE_SIZE })}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
