import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import SettingsCard from '@/components/SettingsCard'
import { useNetToolsSettings } from '@/hooks/useNetTools'
import type { ApiError } from '@/services/api'
import type { AllowlistEntryError } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { allowlistErrors, lineErrors, RETENTION_MAX_DAYS, RETENTION_MIN_DAYS } from '@/utils/netTools'

interface Props {
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

/** Settings → Network tools: what may be probed, whether the Sentinel
 *  server itself runs tools, and how long run history is kept. Admin-only,
 *  like its API. */
export default function NetToolsSettings({ push }: Props) {
  const { settings, loading, error, save } = useNetToolsSettings()
  // Seeded once from the server, then owned by the form, so nothing typed
  // is overwritten by a later answer.
  const [text, setText] = useState<string | null>(null)
  const [serverEnabled, setServerEnabled] = useState(true)
  const [retention, setRetention] = useState(30)
  const [badEntries, setBadEntries] = useState<AllowlistEntryError[]>([])
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!settings || text !== null) return
    setText((settings.allowlist ?? []).join('\n'))
    setServerEnabled(settings.server_enabled)
    setRetention(settings.retention_days)
  }, [settings, text])

  const lines = useMemo(() => lineErrors(text ?? '', badEntries), [text, badEntries])
  const retentionValid =
    Number.isInteger(retention) && retention >= RETENTION_MIN_DAYS && retention <= RETENTION_MAX_DAYS
  const errorBox = `rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`

  if (text === null) {
    return loading ? (
      <div className="flex items-center gap-2 p-6 text-sm text-slate-400">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading network tools settings…
      </div>
    ) : (
      <div className={errorBox}>{error ?? 'Could not load the network tools settings'}</div>
    )
  }

  const submit = async () => {
    if (!retentionValid) return
    setSaving(true)
    setSaveError(null)
    try {
      const saved = await save({
        allowlist: text.split('\n').map((l) => l.trim()).filter(Boolean),
        server_enabled: serverEnabled,
        retention_days: retention,
      })
      // The server trims, de-duplicates and lower-cases host names: show
      // what it stored.
      setText((saved.allowlist ?? []).join('\n'))
      setBadEntries([])
      push('Network tools settings saved', 'success')
    } catch (err) {
      const e = err as ApiError
      setBadEntries(allowlistErrors(e.details))
      setSaveError(e.message || 'Could not save the network tools settings')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-6">
      <p className="text-sm text-slate-400">
        Admins can always run network tools; other users need it granted on the{' '}
        <Link className="text-primary-400 hover:underline" to="/admin/users">
          Users
        </Link>{' '}
        page. Each server agent is allowed from its Edit form.
      </p>

      <SettingsCard
        title="Allowed targets"
        description="One entry per line: an IPv4 address (10.0.0.5), a subnet no broader than /8 (10.0.0.0/24), a host name (fileserver.example.org) or a wildcard (*.example.org). Nothing can be probed until this list has entries."
      >
        <textarea
          rows={8}
          className="w-full font-mono"
          value={text}
          spellCheck={false}
          aria-label="Allowed targets, one per line"
          onChange={(e) => setText(e.target.value)}
        />
        {lines.length > 0 && (
          <ul className={`space-y-1 text-xs ${colors.error.text}`}>
            {lines.map((l, i) => (
              <li key={`${l.entry}-${i}`}>
                {l.line !== null && `Line ${l.line}: `}
                <span className="font-mono">{l.entry}</span> — {l.message}
              </li>
            ))}
          </ul>
        )}
        <p className="text-xs text-slate-500">
          A host-name or wildcard entry allows whatever address the name resolves to, even one outside every subnet
          listed here (always-blocked addresses excepted).
        </p>
        <p className="text-xs text-slate-500">
          Cloud metadata addresses, 0.0.0.0/8, multicast and broadcast are always blocked. Saving never affects runs
          already in progress.
        </p>
      </SettingsCard>

      <SettingsCard
        title="Sentinel server"
        description="Whether tools may run from the Sentinel server itself, as well as from enabled server agents."
      >
        <label className="flex cursor-pointer items-center gap-3">
          <input
            type="checkbox"
            className="h-4 w-4"
            checked={serverEnabled}
            onChange={(e) => setServerEnabled(e.target.checked)}
          />
          <span className="text-sm">Allow runs from the Sentinel server</span>
        </label>
      </SettingsCard>

      <SettingsCard title="Run history" description="Finished runs are deleted after this many days. Audit entries are kept.">
        <div className="flex items-center gap-2">
          <input
            type="number"
            min={RETENTION_MIN_DAYS}
            max={RETENTION_MAX_DAYS}
            value={retention}
            aria-label="Run history retention in days"
            className="w-32"
            onChange={(e) => setRetention(Number(e.target.value))}
          />
          <span className="text-sm text-slate-400">days</span>
        </div>
        {!retentionValid && (
          <p className={`text-xs ${colors.error.text}`}>
            Must be between {RETENTION_MIN_DAYS} and {RETENTION_MAX_DAYS} days.
          </p>
        )}
      </SettingsCard>

      {saveError && <div className={errorBox}>{saveError}</div>}

      <div className="flex justify-end border-t border-white/10 pt-4">
        <button className="btn-primary" disabled={saving || !retentionValid} onClick={() => void submit()}>
          {saving ? 'Saving…' : 'Save Network Tools Settings'}
        </button>
      </div>
    </div>
  )
}
