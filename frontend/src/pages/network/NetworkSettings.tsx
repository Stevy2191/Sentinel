import { useEffect, useState } from 'react'
import { useAuthContext } from '@/context/AuthContext'
import { useNetworkSettings, type NetworkSettings as Settings } from '@/hooks/useNetworkSettings'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-32 rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-primary-500'

const FIELDS: { key: keyof Settings; label: string; help: string; min: number; max: number; unit: string }[] = [
  { key: 'metrics_raw_retention_days', label: 'Detailed history', help: 'How long 1-minute figures are kept. 5-minute and hourly figures are kept forever.', min: 7, max: 3650, unit: 'days' },
  { key: 'port_util_threshold_pct', label: 'Nearly full at', help: 'A port is flagged when either direction averages this over 5 minutes; it clears 10 points lower.', min: 10, max: 100, unit: '%' },
  { key: 'port_error_threshold_per_min', label: 'Errors rising at', help: 'Errors plus discards per minute, sustained for 5 minutes.', min: 1, max: 1000000, unit: 'per minute' },
  { key: 'port_down_grace_seconds', label: 'Alert when an important port is down for', help: 'Long enough that a device rebooting does not alert.', min: 0, max: 86400, unit: 'seconds' },
]

export default function NetworkSettings() {
  const { currentUser } = useAuthContext()
  const { settings, loading, error, saving, save } = useNetworkSettings()
  const [form, setForm] = useState<Record<string, string>>({})
  const [saved, setSaved] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    if (settings) setForm(Object.fromEntries(FIELDS.map((f) => [f.key, String(settings[f.key])])))
  }, [settings])

  if (!currentUser?.is_admin) {
    return <p className="text-sm text-slate-400">Only administrators change network settings.</p>
  }
  if (loading) return <p className="text-sm text-slate-400">Loading…</p>
  if (error || !settings) return <p className="text-sm text-red-400">{error ?? 'Could not load network settings'}</p>

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaved(false)
    setSaveError(null)
    try {
      await save(Object.fromEntries(FIELDS.map((f) => [f.key, Number(form[f.key])])) as Partial<Settings>)
      setSaved(true)
    } catch (err) {
      setSaveError((err as ApiError).message || 'Could not save')
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">Network settings</h1>
        <p className="mt-2 text-sm text-slate-400">Defaults for every port. Each port can override its thresholds on its own page.</p>
      </div>
      <form className="card max-w-2xl space-y-5 p-6" onSubmit={(e) => void submit(e)}>
        {FIELDS.map((f) => (
          <label key={f.key} className="block space-y-1">
            <span className="text-sm text-slate-200">{f.label}</span>
            <span className="flex items-center gap-2">
              <input
                className={inputCls}
                type="number"
                min={f.min}
                max={f.max}
                required
                value={form[f.key] ?? ''}
                onChange={(e) => setForm((s) => ({ ...s, [f.key]: e.target.value }))}
              />
              <span className="text-sm text-slate-400">{f.unit}</span>
            </span>
            <span className="block text-xs text-slate-500">{f.help}</span>
          </label>
        ))}
        {saveError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{saveError}</div>}
        <div className="flex items-center gap-3">
          <button type="submit" className="btn-primary" disabled={saving}>
            Save
          </button>
          {saved && <span className="text-sm text-emerald-400">Saved</span>}
        </div>
      </form>
    </div>
  )
}
