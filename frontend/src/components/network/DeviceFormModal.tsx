import { useState } from 'react'
import { CheckCircle2, X, XCircle } from 'lucide-react'
import { useSites } from '@/hooks/useSites'
import { useSiteCredentialOptions } from '@/hooks/useSnmpCredentials'
import { useDeviceActions, type Device, type DeviceInput, type TestResult } from '@/hooks/useDevices'
import NotificationChannelPicker from '@/components/NotificationChannelPicker'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  initial?: Device
  /** Preselected site when adding from a site page. */
  siteId?: string
  onClose: () => void
  onSaved: (d: Device) => void
}

export default function DeviceFormModal({ initial, siteId, onClose, onSaved }: Props) {
  const { sites } = useSites()
  const { create, update, test, busy } = useDeviceActions()
  const [form, setForm] = useState<DeviceInput>({
    site_id: initial?.site_id ?? siteId ?? '',
    credential_id: initial?.credential_id ?? '',
    name: initial?.name ?? '',
    host: initial?.host ?? '',
    port: initial?.port ?? 161,
    enabled: initial?.enabled ?? true,
    poll_interval: initial?.poll_interval ?? 60,
    timeout_ms: initial?.timeout_ms ?? 3000,
    retries: initial?.retries ?? 1,
    notify_channels: initial?.notify_channels ?? null,
  })
  const { options } = useSiteCredentialOptions(form.site_id || undefined, !!form.site_id)
  const [result, setResult] = useState<TestResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const set = <K extends keyof DeviceInput>(k: K, v: DeviceInput[K]) => setForm((f) => ({ ...f, [k]: v }))

  const runTest = async () => {
    setResult(null)
    setError(null)
    try {
      const r = await test({
        site_id: form.site_id,
        credential_id: form.credential_id,
        host: form.host,
        port: form.port,
        timeout_ms: form.timeout_ms,
        retries: form.retries,
      })
      setResult(r)
      if (r.ok && r.system?.name && !form.name) set('name', r.system.name)
    } catch (err) {
      setError((err as ApiError).message || 'Test failed')
    }
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      onSaved(initial ? await update(initial.id, form) : await create(form))
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the device')
    }
  }

  const ready = form.site_id && form.credential_id && form.host.trim()

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-lg space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit device' : 'Add device'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Site</span>
          <select className={inputCls} value={form.site_id} onChange={(e) => { set('site_id', e.target.value); set('credential_id', '') }} required>
            <option value="">Choose a site…</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>

        <div className="grid grid-cols-[1fr_7rem] gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Address</span>
            <input className={inputCls} value={form.host} onChange={(e) => set('host', e.target.value)} placeholder="10.20.0.2 or core-sw-1.example" required />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Port</span>
            <input type="number" min={1} max={65535} className={inputCls} value={form.port} onChange={(e) => set('port', Number(e.target.value))} />
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Credential profile</span>
          <select className={inputCls} value={form.credential_id} onChange={(e) => set('credential_id', e.target.value)} required disabled={!form.site_id}>
            <option value="">{form.site_id ? 'Choose a profile…' : 'Choose a site first'}</option>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.name} (v{o.version})
              </option>
            ))}
          </select>
        </label>

        <div className="flex items-center gap-3">
          <button type="button" className="btn-secondary" disabled={!ready || busy} onClick={() => void runTest()}>
            {busy ? 'Testing…' : 'Test connection'}
          </button>
          {result &&
            (result.ok ? (
              <span className="flex items-center gap-1.5 text-sm text-emerald-400">
                <CheckCircle2 className="h-4 w-4" /> {result.system?.name || 'Answered'}
                {result.vendor && <span className="text-slate-400">· {result.vendor}</span>}
              </span>
            ) : (
              <span className="flex items-center gap-1.5 text-sm text-red-400">
                <XCircle className="h-4 w-4" /> {result.error}
              </span>
            ))}
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">
            Name <span className="text-slate-500">(optional — taken from the device if blank)</span>
          </span>
          <input className={inputCls} value={form.name} onChange={(e) => set('name', e.target.value)} maxLength={255} />
        </label>

        <details className="rounded-md border border-white/10 p-3">
          <summary className="cursor-pointer text-sm text-slate-300">Polling</summary>
          <div className="mt-3 grid grid-cols-3 gap-3">
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Every (s)</span>
              <input type="number" min={10} max={3600} className={inputCls} value={form.poll_interval} onChange={(e) => set('poll_interval', Number(e.target.value))} />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Timeout (ms)</span>
              <input type="number" min={200} max={30000} className={inputCls} value={form.timeout_ms} onChange={(e) => set('timeout_ms', Number(e.target.value))} />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Retries</span>
              <input type="number" min={1} max={5} className={inputCls} value={form.retries} onChange={(e) => set('retries', Number(e.target.value))} />
            </label>
          </div>
        </details>

        <div className="space-y-1">
          <span className="text-sm text-slate-300">Alert channels</span>
          <NotificationChannelPicker value={form.notify_channels} onChange={(v) => set('notify_channels', v)} />
        </div>

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !ready}>
            {initial ? 'Save' : 'Add device'}
          </button>
        </div>
      </form>
    </div>
  )
}
