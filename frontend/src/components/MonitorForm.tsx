import { useState } from 'react'
import NotificationChannelPicker from '@/components/NotificationChannelPicker'
import type { ApiError } from '@/services/api'
import type { Monitor, MonitorInput, MonitorType } from '@/types'

export interface MonitorFormValues {
  name: string
  type: MonitorType
  url: string
  method: string
  headers: string
  body: string
  interval_seconds: number
  timeout_seconds: number
  retries: number
  tags: string
  /** null = all channels, [] = none, [...] = only those. */
  notify_channels: string[] | null
}

export const emptyMonitorForm: MonitorFormValues = {
  name: '',
  type: 'http',
  url: '',
  method: 'GET',
  headers: '',
  body: '',
  interval_seconds: 60,
  timeout_seconds: 10,
  retries: 3,
  tags: '',
  notify_channels: null,
}

/** Build form values from an existing monitor (for editing). */
export function monitorToForm(m: Monitor): MonitorFormValues {
  return {
    name: m.name,
    type: m.type,
    url: m.url,
    method: m.method || 'GET',
    headers: m.headers ? JSON.stringify(m.headers, null, 2) : '',
    body: m.body || '',
    interval_seconds: m.interval_seconds,
    timeout_seconds: m.timeout_seconds,
    retries: m.retries,
    tags: (m.tags ?? []).join(', '),
    notify_channels: m.notify_channels ?? null,
  }
}

const TYPES: { value: MonitorType; label: string }[] = [
  { value: 'http', label: 'HTTP' },
  { value: 'tcp', label: 'TCP' },
  { value: 'ping', label: 'Ping' },
  { value: 'dns', label: 'DNS' },
]

const urlHelp: Record<MonitorType, string> = {
  http: 'Full URL, e.g. https://example.com/health',
  tcp: 'host:port, e.g. example.com:443',
  ping: 'Hostname or IP, e.g. example.com',
  dns: 'Hostname to resolve, e.g. example.com',
  webhook: 'Endpoint URL',
}

export type MonitorFormErrors = Partial<Record<keyof MonitorFormValues, string>>
type Errors = MonitorFormErrors

export function validateMonitorForm(v: MonitorFormValues): Errors {
  const e: Errors = {}
  const name = v.name.trim()
  if (name.length < 3 || name.length > 255) e.name = 'Name must be 3–255 characters'
  if (!v.url.trim()) e.url = 'URL is required'
  if (!Number.isInteger(v.interval_seconds) || v.interval_seconds < 10 || v.interval_seconds > 3600)
    e.interval_seconds = 'Interval must be 10–3600 seconds'
  if (!Number.isInteger(v.timeout_seconds) || v.timeout_seconds < 1 || v.timeout_seconds > 60)
    e.timeout_seconds = 'Timeout must be 1–60 seconds'
  else if (v.timeout_seconds >= v.interval_seconds)
    e.timeout_seconds = 'Timeout must be less than the interval'
  if (!Number.isInteger(v.retries) || v.retries < 0 || v.retries > 10)
    e.retries = 'Retries must be 0–10'
  if (v.type === 'http' && v.headers.trim()) {
    try {
      const parsed = JSON.parse(v.headers)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
        e.headers = 'Headers must be a JSON object'
    } catch {
      e.headers = 'Headers must be valid JSON'
    }
  }
  return e
}

export function monitorFormToInput(v: MonitorFormValues): MonitorInput {
  const input: MonitorInput = {
    name: v.name.trim(),
    type: v.type,
    url: v.url.trim(),
    interval_seconds: v.interval_seconds,
    timeout_seconds: v.timeout_seconds,
    retries: v.retries,
    enabled: true,
  }
  if (v.type === 'http') {
    input.method = v.method
    if (v.body.trim()) input.body = v.body
    if (v.headers.trim()) input.headers = JSON.parse(v.headers) as Record<string, string>
  }
  const tags = v.tags.split(',').map((t) => t.trim()).filter(Boolean)
  if (tags.length) input.tags = tags
  input.notify_channels = v.notify_channels
  return input
}

interface Props {
  initialValues?: Partial<MonitorFormValues>
  onSubmit: (input: MonitorInput) => Promise<void> | void
  isLoading?: boolean
  error?: ApiError | null
  submitLabel?: string
  onCancel?: () => void
}

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

function Field({
  label,
  required,
  help,
  error,
  children,
}: {
  label: string
  required?: boolean
  help?: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium">
        {label}
        {required && <span className="ml-0.5 text-error-500">*</span>}
      </span>
      {children}
      {error ? (
        <span className="mt-1 block text-xs text-error-600">{error}</span>
      ) : help ? (
        <span className="mt-1 block text-xs text-slate-500">{help}</span>
      ) : null}
    </label>
  )
}

export default function MonitorForm({
  initialValues,
  onSubmit,
  isLoading,
  error,
  submitLabel = 'Save Monitor',
  onCancel,
}: Props) {
  const [values, setValues] = useState<MonitorFormValues>({
    ...emptyMonitorForm,
    ...initialValues,
  })
  const [errors, setErrors] = useState<Errors>({})

  const set = <K extends keyof MonitorFormValues>(key: K, value: MonitorFormValues[K]) =>
    setValues((v) => ({ ...v, [key]: value }))

  const handleSubmit = (ev: React.FormEvent) => {
    ev.preventDefault()
    const errs = validateMonitorForm(values)
    setErrors(errs)
    if (Object.keys(errs).length > 0) return
    void onSubmit(monitorFormToInput(values))
  }

  const isHttp = values.type === 'http'

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="card space-y-4 p-5">
        <Field label="Name" required error={errors.name}>
          <input
            className={inputCls}
            value={values.name}
            onChange={(e) => set('name', e.target.value)}
            placeholder="My API"
          />
        </Field>

        <Field label="Type" required help="Determines how the endpoint is checked">
          <div className="flex flex-wrap gap-2">
            {TYPES.map((t) => (
              <label
                key={t.value}
                className={`cursor-pointer rounded-md border px-4 py-2 text-sm ${
                  values.type === t.value
                    ? 'border-emerald-500/50 bg-emerald-500/10 text-emerald-300'
                    : 'border-white/10'
                }`}
              >
                <input
                  type="radio"
                  name="type"
                  className="sr-only"
                  checked={values.type === t.value}
                  onChange={() => set('type', t.value)}
                />
                {t.label}
              </label>
            ))}
          </div>
        </Field>

        <Field label="URL / Target" required help={urlHelp[values.type]} error={errors.url}>
          <input
            className={inputCls}
            value={values.url}
            onChange={(e) => set('url', e.target.value)}
            placeholder={urlHelp[values.type]}
          />
        </Field>
      </div>

      {isHttp && (
        <div className="card space-y-4 p-5">
          <h3 className="font-semibold">HTTP Options</h3>
          <Field label="Method">
            <select
              className={inputCls}
              value={values.method}
              onChange={(e) => set('method', e.target.value)}
            >
              <option>GET</option>
              <option>POST</option>
              <option>HEAD</option>
              <option>PUT</option>
            </select>
          </Field>
          <Field label="Headers" help="Optional JSON object" error={errors.headers}>
            <textarea
              className={`${inputCls} font-mono`}
              rows={3}
              value={values.headers}
              onChange={(e) => set('headers', e.target.value)}
              placeholder='{"Authorization": "Bearer …"}'
            />
          </Field>
          <Field label="Body" help="Optional request body">
            <textarea
              className={`${inputCls} font-mono`}
              rows={3}
              value={values.body}
              onChange={(e) => set('body', e.target.value)}
            />
          </Field>
        </div>
      )}

      <div className="card grid grid-cols-1 gap-4 p-5 sm:grid-cols-3">
        <Field label="Check interval" required help="Seconds (10–3600)" error={errors.interval_seconds}>
          <input
            type="number"
            className={inputCls}
            value={values.interval_seconds}
            onChange={(e) => set('interval_seconds', Number(e.target.value))}
          />
        </Field>
        <Field label="Timeout" required help="Seconds (1–60)" error={errors.timeout_seconds}>
          <input
            type="number"
            className={inputCls}
            value={values.timeout_seconds}
            onChange={(e) => set('timeout_seconds', Number(e.target.value))}
          />
        </Field>
        <Field label="Retries" required help="0–10" error={errors.retries}>
          <input
            type="number"
            className={inputCls}
            value={values.retries}
            onChange={(e) => set('retries', Number(e.target.value))}
          />
        </Field>
      </div>

      <div className="card space-y-4 p-5">
        <Field label="Notifications" help="Where alerts for this monitor are sent">
          <NotificationChannelPicker
            value={values.notify_channels}
            onChange={(v) => set('notify_channels', v)}
          />
        </Field>
        <Field label="Tags" help="Comma-separated, e.g. prod, api">
          <input
            className={inputCls}
            value={values.tags}
            onChange={(e) => set('tags', e.target.value)}
            placeholder="prod, critical"
          />
        </Field>
      </div>

      {error && (
        <div className="card border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">
          {error.message}
        </div>
      )}

      <div className="flex gap-2">
        <button type="submit" className="btn-primary" disabled={isLoading}>
          {isLoading ? 'Saving…' : submitLabel}
        </button>
        {onCancel && (
          <button type="button" className="btn-secondary" onClick={onCancel}>
            Cancel
          </button>
        )}
      </div>
    </form>
  )
}
