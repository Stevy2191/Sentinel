import { useEffect, useMemo, useRef, useState } from 'react'
import { X, Loader2 } from 'lucide-react'
import { useCreateMonitor } from '@/hooks/useMonitors'
import { useAppConfig } from '@/context/AppConfigContext'
import type { ApiError } from '@/services/api'
import type { Monitor, MonitorType } from '@/types'

// Mirrors the backend's monitor validation (models/monitor.go), so the form
// rejects what the API would reject rather than round-tripping to find out.
const MIN_INTERVAL = 10
const MAX_INTERVAL = 3600
const MIN_TIMEOUT = 1
const MAX_TIMEOUT = 300

type TypeKey = 'dns' | 'http' | 'ping' | 'tcp'

const TYPE_OPTIONS: { value: TypeKey; label: string }[] = [
  { value: 'dns', label: 'DNS' },
  { value: 'http', label: 'HTTP' },
  { value: 'ping', label: 'PING' },
  { value: 'tcp', label: 'TCP' },
]

const RETRY_OPTIONS = [1, 2, 3, 5]

const BASE_INTERVALS = [30, 60, 300, 600, 1800, 3600]

/**
 * Parses a spoken-English duration into seconds: "45 seconds", "2 minutes",
 * "1.5 hours", "90s", or a bare number (read as seconds).
 *
 * Returns null when it cannot tell what was meant. That is deliberately
 * distinct from "understood but out of range", which the caller reports with
 * the actual bound so the person knows what to type instead.
 */
export function parseDuration(raw: string): number | null {
  const text = raw.trim().toLowerCase()
  if (!text) return null
  const m = /^(\d+(?:\.\d+)?)\s*(s|sec|secs|second|seconds|m|min|mins|minute|minutes|h|hr|hrs|hour|hours)?$/.exec(
    text
  )
  if (!m) return null
  const value = Number(m[1])
  if (!Number.isFinite(value) || value <= 0) return null
  const unit = m[2] ?? 's'
  const multiplier = unit.startsWith('h') ? 3600 : unit.startsWith('m') ? 60 : 1
  const seconds = Math.round(value * multiplier)
  return seconds > 0 ? seconds : null
}

function intervalLabel(sec: number): string {
  if (sec < 60) return `${sec} seconds`
  if (sec < 3600) {
    const m = sec / 60
    return m === 1 ? '1 minute' : `${Number.isInteger(m) ? m : m.toFixed(1)} minutes`
  }
  const h = sec / 3600
  return h === 1 ? '1 hour' : `${Number.isInteger(h) ? h : h.toFixed(1)} hours`
}

/** Placeholder and hint for the target field, which means something different per type. */
const TARGET_HELP: Record<TypeKey, { placeholder: string; hint: string }> = {
  http: { placeholder: 'https://example.com', hint: 'Full URL including protocol (http:// or https://)' },
  dns: { placeholder: 'example.com', hint: 'Domain name to resolve' },
  ping: { placeholder: '192.168.1.1', hint: 'IP address or hostname to ping' },
  tcp: { placeholder: '192.168.1.1:5432', hint: 'IP address or hostname and port (HOST:PORT)' },
}

interface FormState {
  name: string
  type: TypeKey
  description: string
  target: string
  /** A preset in seconds, or 'custom' while the free-text field is in use. */
  interval: number | 'custom'
  customInterval: string
  timeout: number
  retryAttempts: number
  enableNotifications: boolean
  enableSSLVerify: boolean
}

interface Errors {
  name?: string
  target?: string
  interval?: string
  timeout?: string
}

const field =
  'w-full rounded-lg border border-white/10 bg-slate-800/50 px-4 py-2 text-white placeholder-slate-500 transition focus:border-white/30 focus:outline-none'

/** Toggle row, sized to match the reference (w-12 h-7 track, h-5 w-5 knob). */
function Switch({
  label,
  hint,
  checked,
  onChange,
  disabled = false,
}: {
  label: string
  hint: string
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
}) {
  return (
    <div
      className={`flex items-center justify-between gap-4 rounded-lg border border-white/10 bg-slate-800/40 p-4 ${
        disabled ? 'opacity-60' : ''
      }`}
    >
      <div className="min-w-0">
        <p className="text-sm font-medium text-white">{label}</p>
        <p className="text-xs text-slate-500">{hint}</p>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`relative h-7 w-12 shrink-0 rounded-full transition-colors disabled:cursor-not-allowed ${
          checked ? 'bg-emerald-600' : 'bg-slate-600'
        }`}
      >
        <span
          className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white transition-transform ${
            checked ? 'translate-x-5' : 'translate-x-0'
          }`}
        />
      </button>
    </div>
  )
}

interface Props {
  isOpen: boolean
  onClose: () => void
  /** Called with the created monitor so the caller can refresh its list. */
  onCreated?: (monitor: Monitor) => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

/**
 * CreateMonitorModal — the quick "add a service" dialog behind the + New button.
 *
 * It deliberately covers only the common case. Headers, request body, HTTP
 * method, tags, groups and per-channel notification routing are not here; the
 * full form at /monitors/create still owns those, and this links to it.
 */
export default function CreateMonitorModal({ isOpen, onClose, onCreated, push }: Props) {
  const { defaultCheckInterval } = useAppConfig()
  const { create, loading } = useCreateMonitor()
  const dialogRef = useRef<HTMLDivElement>(null)
  const firstFieldRef = useRef<HTMLInputElement>(null)

  const blank = useMemo<FormState>(
    () => ({
      name: '',
      type: 'http',
      description: '',
      target: '',
      // Starts at the instance default from Settings → System, so this dialog
      // agrees with the other create paths.
      interval: defaultCheckInterval,
      customInterval: '',
      timeout: 10,
      retryAttempts: 3,
      enableNotifications: true,
      // Verification on by default, matching the backend column default.
      enableSSLVerify: true,
    }),
    [defaultCheckInterval]
  )

  const [form, setForm] = useState<FormState>(blank)
  const [errors, setErrors] = useState<Errors>({})

  // Reset on each open so a cancelled attempt never leaks into the next one.
  useEffect(() => {
    if (!isOpen) return
    setForm(blank)
    setErrors({})
    const t = window.setTimeout(() => firstFieldRef.current?.focus(), 50)
    return () => window.clearTimeout(t)
  }, [isOpen, blank])

  // Escape closes, and focus is kept inside the dialog while it is open.
  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !loading) {
        onClose()
        return
      }
      if (e.key !== 'Tab' || !dialogRef.current) return
      const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
        'button, input, select, textarea, a[href]'
      )
      const list = Array.from(focusable).filter((el) => !el.hasAttribute('disabled'))
      if (list.length === 0) return
      const first = list[0]
      const last = list[list.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    // The page behind must not scroll while a modal is over it.
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [isOpen, loading, onClose])

  // The instance default may not be one of the stock choices; offer it anyway
  // rather than silently snapping the value to something else.
  const intervalOptions = useMemo(() => {
    const set = new Set<number>([...BASE_INTERVALS, defaultCheckInterval])
    if (typeof form.interval === 'number') set.add(form.interval)
    return Array.from(set)
      .filter((n) => n >= MIN_INTERVAL && n <= MAX_INTERVAL)
      .sort((a, b) => a - b)
  }, [defaultCheckInterval, form.interval])

  // The interval the request will actually carry, whether it came from the
  // preset list or the free-text field. null means the text is unparseable.
  const effectiveInterval =
    form.interval === 'custom' ? parseDuration(form.customInterval) : form.interval

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((f) => ({ ...f, [key]: value }))
    if (key in errors) setErrors((e) => ({ ...e, [key]: undefined }))
  }

  function validate(f: FormState, interval: number | null): Errors {
    const e: Errors = {}
    if (!f.name.trim()) e.name = 'A name is required'
    const target = f.target.trim()
    if (!target) {
      e.target = 'A target is required'
    } else if (f.type === 'http' && !/^https?:\/\/.+/i.test(target)) {
      e.target = 'Must start with http:// or https://'
    } else if (f.type === 'tcp' && !/^[^\s:]+:\d{1,5}$/.test(target)) {
      e.target = 'Must be HOST:PORT, e.g. db.example.com:5432'
    } else if (f.type !== 'http' && /\s/.test(target)) {
      e.target = 'Must not contain spaces'
    }
    if (interval === null) {
      e.interval = f.customInterval.trim()
        ? 'Could not read that — try "45 seconds" or "2 minutes"'
        : 'An interval is required'
    } else if (interval < MIN_INTERVAL || interval > MAX_INTERVAL) {
      e.interval = `Must be between ${intervalLabel(MIN_INTERVAL)} and ${intervalLabel(MAX_INTERVAL)}`
    }

    if (f.timeout < MIN_TIMEOUT || f.timeout > MAX_TIMEOUT) {
      e.timeout = `Must be between ${MIN_TIMEOUT} and ${MAX_TIMEOUT} seconds`
    } else if (interval !== null && f.timeout >= interval) {
      // The backend rejects this outright; catching it here explains why.
      e.timeout = 'Must be less than the check interval'
    }
    return e
  }

  const submit = async () => {
    const interval = effectiveInterval
    const found = validate(form, interval)
    setErrors(found)
    if (Object.keys(found).length > 0 || interval === null) return

    try {
      const created = await create({
        name: form.name.trim(),
        description: form.description.trim(),
        type: form.type as MonitorType,
        url: form.target.trim(),
        interval_seconds: interval,
        timeout_seconds: form.timeout,
        retries: form.retryAttempts,
        // Only meaningful for HTTPS checks, but sent for every type so the
        // stored value matches what the form showed.
        ssl_verify: form.enableSSLVerify,
        // null = every enabled channel (the default), [] = alerts off for this
        // monitor. Which channels specifically is set on the monitor's page.
        notify_channels: form.enableNotifications ? null : [],
      })
      push(`Monitor "${created.name}" created`, 'success')
      setForm(blank)
      setErrors({})
      onCreated?.(created)
      onClose()
    } catch (err) {
      push((err as ApiError).message || 'Could not create the monitor', 'error')
    }
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => {
        // Only a click that both starts and ends on the backdrop closes it, so
        // a drag that ends outside the dialog does not discard the form.
        if (e.target === e.currentTarget && !loading) onClose()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-monitor-title"
        className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-white/10 bg-slate-900/95"
      >
        {/* Header */}
        <div className="flex shrink-0 items-center justify-between border-b border-white/10 p-6">
          <div>
            <h2 id="create-monitor-title" className="text-2xl font-light text-white">
              Create New Monitor
            </h2>
            <p className="mt-1 text-sm text-slate-400">Add a new service to monitor</p>
          </div>
          <button
            onClick={onClose}
            disabled={loading}
            className="rounded p-1 text-slate-400 transition hover:text-white disabled:opacity-50"
            aria-label="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Form — the body scrolls, the header and footer stay put. */}
        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
          <section>
            <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Basic Information
            </h3>
            <div className="space-y-4">
              <div>
                <label htmlFor="cm-name" className="mb-1 block text-sm font-medium text-white">
                  Service Name
                </label>
                <input
                  id="cm-name"
                  ref={firstFieldRef}
                  type="text"
                  value={form.name}
                  onChange={(e) => set('name', e.target.value)}
                  placeholder="e.g., Website API, Database Server"
                  aria-invalid={!!errors.name}
                  className={`${field} ${errors.name ? 'border-red-500/60' : ''}`}
                />
                <p className={`mt-1 text-xs ${errors.name ? 'text-red-400' : 'text-slate-500'}`}>
                  {errors.name ?? 'Give your monitor a descriptive name'}
                </p>
              </div>

              <div>
                <label htmlFor="cm-type" className="mb-1 block text-sm font-medium text-white">
                  Service Type
                </label>
                <select
                  id="cm-type"
                  value={form.type}
                  onChange={(e) => {
                    // The target means something different per type, so a stale
                    // value from the previous type would only mislead.
                    setForm((f) => ({ ...f, type: e.target.value as TypeKey, target: '' }))
                    setErrors((x) => ({ ...x, target: undefined }))
                  }}
                  className={`${field} cursor-pointer appearance-none`}
                >
                  {TYPE_OPTIONS.map((t) => (
                    <option key={t.value} value={t.value}>
                      {t.label}
                    </option>
                  ))}
                </select>
                <p className="mt-1 text-xs text-slate-500">Select the type of monitoring check</p>
              </div>

              <div>
                <label htmlFor="cm-desc" className="mb-1 block text-sm font-medium text-white">
                  Description (Optional)
                </label>
                <textarea
                  id="cm-desc"
                  value={form.description}
                  onChange={(e) => set('description', e.target.value)}
                  placeholder="Add notes about this monitor…"
                  rows={2}
                  className={`${field} resize-none`}
                />
              </div>
            </div>
          </section>

          <div className="border-t border-white/10" />

          <section>
            <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Configuration
            </h3>
            <div className="space-y-4">
              <div>
                <label htmlFor="cm-target" className="mb-1 block text-sm font-medium text-white">
                  Target URL/Host
                </label>
                <input
                  id="cm-target"
                  type="text"
                  value={form.target}
                  onChange={(e) => set('target', e.target.value)}
                  placeholder={TARGET_HELP[form.type].placeholder}
                  aria-invalid={!!errors.target}
                  className={`${field} ${errors.target ? 'border-red-500/60' : ''}`}
                />
                <p className={`mt-1 text-xs ${errors.target ? 'text-red-400' : 'text-slate-500'}`}>
                  {errors.target ?? TARGET_HELP[form.type].hint}
                </p>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div>
                  <label htmlFor="cm-interval" className="mb-1 block text-sm font-medium text-white">
                    Check Interval
                  </label>
                  {form.interval === 'custom' ? (
                    <input
                      id="cm-interval"
                      type="text"
                      autoFocus
                      value={form.customInterval}
                      onChange={(e) => set('customInterval', e.target.value)}
                      placeholder="e.g., 45 seconds, 2 minutes, 15 minutes"
                      aria-invalid={!!errors.interval}
                      className={`${field} ${errors.interval ? 'border-red-500/60' : ''}`}
                    />
                  ) : (
                    <select
                      id="cm-interval"
                      value={form.interval}
                      onChange={(e) => {
                        const v = e.target.value
                        setForm((f) => ({
                          ...f,
                          interval: v === 'custom' ? 'custom' : Number(v),
                          // Seed the box with the preset that was showing, so
                          // switching to Custom starts from where you were.
                          customInterval:
                            v === 'custom' && typeof f.interval === 'number'
                              ? String(f.interval)
                              : f.customInterval,
                        }))
                        setErrors((x) => ({ ...x, interval: undefined, timeout: undefined }))
                      }}
                      className={`${field} cursor-pointer appearance-none`}
                    >
                      {intervalOptions.map((sec) => (
                        <option key={sec} value={sec}>
                          {intervalLabel(sec)}
                        </option>
                      ))}
                      <option value="custom">Custom</option>
                    </select>
                  )}
                  <p className={`mt-1 text-xs ${errors.interval ? 'text-red-400' : 'text-slate-500'}`}>
                    {errors.interval ??
                      (form.interval === 'custom'
                        ? effectiveInterval !== null
                          ? `= ${effectiveInterval} seconds`
                          : 'Seconds, or a phrase like "2 minutes"'
                        : 'How often to check status')}
                  </p>
                  {form.interval === 'custom' && (
                    <button
                      type="button"
                      onClick={() => {
                        setForm((f) => ({
                          ...f,
                          interval: parseDuration(f.customInterval) ?? defaultCheckInterval,
                        }))
                        setErrors((x) => ({ ...x, interval: undefined }))
                      }}
                      className="mt-1 text-xs text-slate-400 underline-offset-2 transition hover:text-white hover:underline"
                    >
                      Use a preset instead
                    </button>
                  )}
                </div>

                <div>
                  <label htmlFor="cm-retries" className="mb-1 block text-sm font-medium text-white">
                    Retry Attempts
                  </label>
                  <select
                    id="cm-retries"
                    value={form.retryAttempts}
                    onChange={(e) => set('retryAttempts', Number(e.target.value))}
                    className={`${field} cursor-pointer appearance-none`}
                  >
                    {RETRY_OPTIONS.map((n) => (
                      <option key={n} value={n}>
                        {n === 1 ? '1 attempt' : `${n} attempts`}
                      </option>
                    ))}
                  </select>
                  <p className="mt-1 text-xs text-slate-500">Before marking as down</p>
                </div>
              </div>

              <div>
                <label htmlFor="cm-timeout" className="mb-1 block text-sm font-medium text-white">
                  Timeout (seconds)
                </label>
                <input
                  id="cm-timeout"
                  type="number"
                  min={MIN_TIMEOUT}
                  max={MAX_TIMEOUT}
                  value={form.timeout}
                  onChange={(e) => set('timeout', Number(e.target.value))}
                  aria-invalid={!!errors.timeout}
                  className={`${field} ${errors.timeout ? 'border-red-500/60' : ''}`}
                />
                <p className={`mt-1 text-xs ${errors.timeout ? 'text-red-400' : 'text-slate-500'}`}>
                  {errors.timeout ?? 'Maximum time to wait for a response'}
                </p>
              </div>
            </div>
          </section>

          <div className="border-t border-white/10" />

          <section>
            <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Notifications
            </h3>
            <div className="space-y-4">
              <Switch
                label="Enable Alerts"
                hint="Send to every configured channel when this monitor changes state"
                checked={form.enableNotifications}
                onChange={(v) => set('enableNotifications', v)}
              />
              <Switch
                label="Verify SSL Certificate"
                hint={
                  form.type === 'http'
                    ? 'Check SSL certificate validity (for HTTPS)'
                    : 'Only applies to HTTPS checks — no effect on a ' + form.type.toUpperCase() + ' monitor'
                }
                checked={form.enableSSLVerify}
                onChange={(v) => set('enableSSLVerify', v)}
                disabled={form.type !== 'http'}
              />
              {form.type === 'http' && !form.enableSSLVerify && (
                <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-300">
                  With verification off, an expired, self-signed or wrong-host certificate will not
                  be reported — use this only for a host whose certificate you control.
                </p>
              )}
            </div>
          </section>
        </div>

        {/* Footer */}
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-3 border-t border-white/10 p-6">
          <button
            onClick={onClose}
            disabled={loading}
            className="rounded-lg border border-white/20 px-6 py-2 text-sm font-medium text-white transition hover:bg-white/5 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={() => void submit()}
            disabled={loading}
            className="inline-flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-600 to-emerald-500 px-6 py-2 text-sm font-medium text-white transition hover:from-emerald-500 hover:to-emerald-400 disabled:opacity-60"
          >
            {loading && <Loader2 className="h-4 w-4 animate-spin" />}
            {loading ? 'Creating…' : 'Create Monitor'}
          </button>
        </div>
      </div>
    </div>
  )
}
