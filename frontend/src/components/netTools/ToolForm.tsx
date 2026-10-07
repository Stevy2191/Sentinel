import { useId } from 'react'
import { Loader2, Play, Square } from 'lucide-react'
import type { NetTool, ToolParams, VantageView } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { DNS_RECORD_TYPES, NUMBER_FIELDS, paramError, vantageKey, type NumberKey } from '@/utils/netTools'
import TargetInput from './TargetInput'
import VantageSelect from './VantageSelect'

interface Props {
  tool: NetTool
  target: string
  onTarget: (v: string) => void
  /** A vantageKey. */
  vantage: string
  onVantage: (key: string) => void
  vantages: VantageView[]
  /** This tool's parameters. */
  params: ToolParams
  onParams: (p: ToolParams) => void
  /** A run started here is queued or running. */
  running: boolean
  /** A create or cancel request is in flight. */
  busy: boolean
  onRun: () => void
  onCancel: () => void
}

/** One tool's form: target, where to run from, the tool's own options, and
 *  Run (or Cancel while a run is going). */
export default function ToolForm(props: Props) {
  const { tool, target, onTarget, vantage, onVantage, vantages, params, onParams, running, busy, onRun, onCancel } = props
  const id = useId()
  const problem = paramError(tool, params)
  const chosen = vantages.find((v) => vantageKey(v) === vantage)
  const canRun = !busy && !running && target.trim() !== '' && problem === null && !!chosen?.ready

  const setNumber = (key: NumberKey, raw: string) => {
    const next: ToolParams = { ...params }
    next[key] = raw === '' ? undefined : Number(raw)
    onParams(next)
  }

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (canRun) onRun()
      }}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <TargetInput tool={tool} value={target} onChange={onTarget} />
        <VantageSelect vantages={vantages} value={vantage} onChange={onVantage} disabled={running} />
      </div>

      {tool === 'dns' && (
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label htmlFor={`${id}-rtype`} className="mb-1 block text-sm font-medium text-white">
              Record type
            </label>
            <select
              id={`${id}-rtype`}
              className="w-full"
              value={params.record_type ?? 'A'}
              onChange={(e) => onParams({ ...params, record_type: e.target.value })}
            >
              {DNS_RECORD_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor={`${id}-server`} className="mb-1 block text-sm font-medium text-white">
              DNS server
            </label>
            <input
              id={`${id}-server`}
              className="w-full font-mono"
              value={params.server ?? ''}
              placeholder="System resolver"
              spellCheck={false}
              onChange={(e) => onParams({ ...params, server: e.target.value })}
            />
            <p className="mt-1 text-xs text-slate-500">
              Empty asks the resolver of the machine it runs from. A named server (10.0.0.53 or 10.0.0.53:5353)
              must be on the allowlist.
            </p>
          </div>
        </div>
      )}

      {tool === 'tcp' && (
        <div>
          <label htmlFor={`${id}-ports`} className="mb-1 block text-sm font-medium text-white">
            Ports
          </label>
          <input
            id={`${id}-ports`}
            className="w-full font-mono"
            value={params.ports ?? ''}
            placeholder="common"
            spellCheck={false}
            onChange={(e) => onParams({ ...params, ports: e.target.value })}
          />
          <p className="mt-1 text-xs text-slate-500">
            &quot;common&quot; checks about 100 well-known ports. Or list ports and ranges, e.g. 22,80,443,8000-8100
            (at most 1024).
          </p>
        </div>
      )}

      {NUMBER_FIELDS[tool].length > 0 && (
        <details className="rounded-lg border border-white/10 bg-slate-900/30 p-3">
          <summary className="cursor-pointer text-sm text-slate-300">Options</summary>
          <div className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {NUMBER_FIELDS[tool].map((f) => (
              <div key={f.key}>
                <label htmlFor={`${id}-${f.key}`} className="mb-1 block text-xs font-medium text-slate-300">
                  {f.label}
                  {f.unit ? ` (${f.unit})` : ''}
                </label>
                <input
                  id={`${id}-${f.key}`}
                  type="number"
                  className="w-full"
                  min={f.min}
                  max={f.max}
                  step={f.step ?? 1}
                  value={params[f.key] ?? ''}
                  onChange={(e) => setNumber(f.key, e.target.value)}
                />
              </div>
            ))}
          </div>
        </details>
      )}

      {problem && <p className={`text-xs ${colors.error.text}`}>{problem}</p>}

      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className="btn-primary" disabled={!canRun}>
          {busy && !running ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />} Run
        </button>
        {running && (
          <button type="button" className="btn-secondary" disabled={busy} onClick={onCancel}>
            <Square className="h-4 w-4" /> Cancel
          </button>
        )}
      </div>
    </form>
  )
}
