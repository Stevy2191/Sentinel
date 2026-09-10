import { useEffect, useMemo, useRef, useState } from 'react'
import { X, Loader2 } from 'lucide-react'
import AgentSettingsFields, {
  validateAgentSettings,
  type AgentSettings,
} from '@/components/AgentSettingsFields'
import { useAgentActions, type Agent } from '@/hooks/useAgents'
import type { ApiError } from '@/services/api'

interface Props {
  agent: Agent
  isOpen: boolean
  onClose: () => void
  /** Called after a successful save so the page can re-read the agent. */
  onSaved: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

function settingsOf(agent: Agent): AgentSettings {
  return {
    name: agent.name,
    osType: agent.os_type,
    ipOverride: agent.ip_address_override ?? '',
    interval: agent.check_interval,
    retries: agent.retry_attempts,
  }
}

/**
 * Edits the settings an operator owns.
 *
 * Credentials are not among them. Rotating a token here would silently break
 * the agent already installed on that host, which would look like the host
 * failing rather than like a setting having been changed.
 */
export default function EditServerAgentModal({ agent, isOpen, onClose, onSaved, push }: Props) {
  const { update, busy } = useAgentActions()
  const firstFieldRef = useRef<HTMLInputElement>(null)

  const initial = useMemo(() => settingsOf(agent), [agent])
  const [values, setValues] = useState<AgentSettings>(initial)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!isOpen) return
    // Re-seeded on open so a cancelled edit does not persist into the next one.
    setValues(initial)
    setError(null)
    const t = window.setTimeout(() => firstFieldRef.current?.focus(), 50)
    return () => window.clearTimeout(t)
  }, [isOpen, initial])

  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [isOpen, busy, onClose])

  const { errors, valid } = validateAgentSettings(values, true)
  const changed =
    values.name !== initial.name ||
    values.osType !== initial.osType ||
    values.ipOverride !== initial.ipOverride ||
    values.interval !== initial.interval ||
    values.retries !== initial.retries

  if (!isOpen) return null

  const save = async () => {
    if (!valid || !changed) return
    setError(null)
    try {
      await update(agent.agent_id, {
        name: values.name.trim(),
        os_type: values.osType,
        check_interval: values.interval,
        retry_attempts: values.retries,
        // Sent even when empty: that is how an override is cleared and the
        // address goes back to whatever the agent detects.
        ip_address_override: values.ipOverride.trim(),
      })
      push(`${values.name.trim()} updated`, 'success')
      onSaved()
      onClose()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the changes')
    }
  }

  // Worth saying: an interval change reaches the agent on its next report
  // rather than immediately, and someone watching the graph would otherwise
  // think the setting had not taken.
  const intervalChanged = values.interval !== initial.interval

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Edit ${agent.name}`}
        className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-white/10 bg-slate-900/95"
      >
        <div className="flex shrink-0 items-start justify-between gap-4 border-b border-white/10 p-6">
          <div className="min-w-0">
            <h2 className="truncate text-2xl font-light text-white">Edit {agent.name}</h2>
            <p className="mt-1 text-sm text-slate-400">
              Settings only — the agent id and token stay as they are
            </p>
          </div>
          <button
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 text-slate-400 transition hover:text-white"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
          <AgentSettingsFields
            values={values}
            onChange={setValues}
            errors={errors}
            firstFieldRef={firstFieldRef}
          />

          {intervalChanged && (
            <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-xs text-slate-400">
              The agent picks up a new interval on its next report, so the change takes effect
              within one collection cycle rather than straight away.
            </p>
          )}

          {error && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
              {error}
            </div>
          )}
        </div>

        <div className="flex shrink-0 items-center justify-end gap-3 border-t border-white/10 p-6">
          <button
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-white/20 px-6 py-2 text-sm font-medium text-white transition hover:bg-white/5 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={() => void save()}
            disabled={busy || !valid || !changed}
            title={!changed ? 'Nothing has been changed' : undefined}
            className="inline-flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-600 to-emerald-500 px-6 py-2 text-sm font-medium text-white transition hover:from-emerald-500 hover:to-emerald-400 disabled:opacity-50"
          >
            {busy && <Loader2 className="h-4 w-4 animate-spin" />}
            Save changes
          </button>
        </div>
      </div>
    </div>
  )
}
