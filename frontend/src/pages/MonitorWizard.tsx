import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  ArrowRight,
  Check,
  Globe,
  Loader2,
  Plug,
  Radio,
  Search,
  X,
} from 'lucide-react'
import api from '@/services/api'
import { useCreateMonitor } from '@/hooks/useMonitors'
import { useMonitorGroups, useMoveMonitorToGroup } from '@/hooks/useMonitorGroups'
import { useToasts, Toaster } from '@/components/Toast'
import NotificationChannelPicker from '@/components/NotificationChannelPicker'
import {
  emptyMonitorForm,
  validateMonitorForm,
  monitorFormToInput,
  type MonitorFormValues,
} from '@/components/MonitorForm'
import type { ApiResponse, MonitorType } from '@/types'

// What the wizard asks, in order. Kept as data so the stepper and the guards
// below cannot drift from each other.
const STEPS = ['Kind', 'Target', 'Schedule', 'Alerts', 'Review'] as const
type StepIndex = 0 | 1 | 2 | 3 | 4

// Monitor types described by what the user is trying to find out, rather than
// by protocol — someone adding their first monitor knows "is my site up",
// not "I need an HTTP GET check".
const KINDS: {
  type: MonitorType
  title: string
  blurb: string
  icon: typeof Globe
  placeholder: string
  hint: string
}[] = [
  {
    type: 'http',
    title: 'Website or API',
    blurb: 'Fetch a URL and check it answers successfully.',
    icon: Globe,
    placeholder: 'https://api.example.com/health',
    hint: 'A full URL including https://. A 2xx response counts as healthy.',
  },
  {
    type: 'tcp',
    title: 'Port',
    blurb: 'Open a TCP connection — databases, mail, SSH.',
    icon: Plug,
    placeholder: 'db.example.com:5432',
    hint: 'host:port. Healthy means the port accepted a connection.',
  },
  {
    type: 'ping',
    title: 'Host reachability',
    blurb: 'Ping a machine to see whether it answers at all.',
    icon: Radio,
    placeholder: 'server.example.com',
    hint: 'A hostname or IP address.',
  },
  {
    type: 'dns',
    title: 'DNS record',
    blurb: 'Check a name still resolves.',
    icon: Search,
    placeholder: 'example.com',
    hint: 'The hostname to resolve.',
  },
]

const INTERVAL_PRESETS = [
  { seconds: 30, label: 'Every 30 seconds', detail: 'Tight feedback, more traffic' },
  { seconds: 60, label: 'Every minute', detail: 'A good default' },
  { seconds: 300, label: 'Every 5 minutes', detail: 'Light touch' },
  { seconds: 900, label: 'Every 15 minutes', detail: 'Background checks' },
]

interface TestOutcome {
  ok: boolean
  status: string
  response_time_ms: number
  status_code: number
  error_message: string
}

/** Turn a probe result into one plain sentence. */
function describeOutcome(o: TestOutcome): string {
  if (o.ok) {
    return o.status_code > 0
      ? `${o.status_code} in ${o.response_time_ms}ms — looks healthy.`
      : `Responded in ${o.response_time_ms}ms — looks healthy.`
  }
  if (o.status === 'timeout') return `Timed out. ${o.error_message}`
  return o.error_message || 'The target did not respond successfully.'
}

function StepDots({ current }: { current: StepIndex }) {
  return (
    <ol className="flex flex-wrap items-center gap-2">
      {STEPS.map((label, i) => {
        const done = i < current
        const active = i === current
        return (
          <li key={label} className="flex items-center gap-2">
            <span
              className="flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-bold"
              style={{
                backgroundColor: done ? 'var(--vs-ecg)' : active ? 'rgba(61,225,255,0.15)' : 'transparent',
                border: `1px solid ${done ? 'var(--vs-ecg)' : active ? 'var(--vs-cyan)' : 'var(--vs-line)'}`,
                color: done ? '#06120c' : active ? 'var(--vs-cyan)' : 'var(--vs-text-dim)',
              }}
            >
              {done ? <Check className="h-3.5 w-3.5" /> : i + 1}
            </span>
            <span
              className="vs-eyebrow"
              style={{ color: active ? 'var(--vs-cyan)' : 'var(--vs-text-dim)' }}
            >
              {label}
            </span>
            {i < STEPS.length - 1 && <span className="mx-1 h-px w-5" style={{ backgroundColor: 'var(--vs-line)' }} />}
          </li>
        )
      })}
    </ol>
  )
}

const inputCls =
  'w-full rounded-md border px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-primary-500'
const inputStyle = {
  borderColor: 'var(--vs-line)',
  backgroundColor: 'var(--vs-panel-2)',
  color: 'var(--vs-text)',
}

/**
 * MonitorWizard — a guided alternative to the single-page form, for people who
 * would rather be asked questions than fill in a schema. Its one real advantage
 * over the form is the connection test on step 2: it probes the target through
 * the backend before anything is saved, so you find out a URL is wrong while
 * you are still looking at it.
 */
export default function MonitorWizard() {
  const navigate = useNavigate()
  const { toasts, push } = useToasts()
  const { create, loading: creating } = useCreateMonitor()
  const { groups } = useMonitorGroups()
  const { move } = useMoveMonitorToGroup()

  const [step, setStep] = useState<StepIndex>(0)
  const [values, setValues] = useState<MonitorFormValues>({ ...emptyMonitorForm })
  const [groupId, setGroupId] = useState<string>('')
  const [testing, setTesting] = useState(false)
  const [outcome, setOutcome] = useState<TestOutcome | null>(null)

  const kind = KINDS.find((k) => k.type === values.type) ?? KINDS[0]
  const set = <K extends keyof MonitorFormValues>(key: K, value: MonitorFormValues[K]) =>
    setValues((v) => ({ ...v, [key]: value }))

  const errors = validateMonitorForm(values)
  // Only the fields a given step is responsible for should block it.
  const canAdvance: Record<StepIndex, boolean> = {
    0: true,
    1: values.url.trim() !== '',
    2: !errors.name && !errors.interval_seconds && !errors.timeout_seconds && !errors.retries,
    3: true,
    4: Object.keys(errors).length === 0,
  }

  const runTest = async () => {
    setTesting(true)
    setOutcome(null)
    try {
      const { data } = await api.post<ApiResponse<TestOutcome>>('/monitors/test-config', {
        type: values.type,
        url: values.url.trim(),
        method: values.type === 'http' ? values.method : undefined,
        timeout_seconds: values.timeout_seconds,
      })
      setOutcome(data.data)
    } catch (err) {
      setOutcome({
        ok: false,
        status: 'failed',
        response_time_ms: 0,
        status_code: 0,
        error_message: (err as { message?: string }).message ?? 'Could not run the test',
      })
    } finally {
      setTesting(false)
    }
  }

  const submit = async () => {
    try {
      const created = await create(monitorFormToInput(values))
      if (groupId) {
        try {
          await move(created.id, groupId)
        } catch {
          // The monitor exists; only the grouping failed, which is recoverable
          // from the dashboard, so don't present this as a failed creation.
          push('Monitor created, but it could not be added to the group', 'info')
        }
      }
      push(`${created.name} is now being monitored`, 'success')
      navigate('/dashboard')
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to create monitor', 'error')
    }
  }

  const back = () => setStep((s) => Math.max(0, s - 1) as StepIndex)
  const next = () => setStep((s) => Math.min(STEPS.length - 1, s + 1) as StepIndex)

  return (
    <div className="mx-auto max-w-3xl space-y-6 pb-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="vs-title text-2xl">New monitor</h1>
        <button className="rd-btn rd-btn-secondary" onClick={() => navigate('/dashboard')}>
          <X className="h-4 w-4" /> Cancel
        </button>
      </div>

      <StepDots current={step} />

      <div className="rd-card p-6" style={{ ['--rd-accent' as string]: 'var(--vs-cyan)' }}>
        {/* ---- 1. Kind ---- */}
        {step === 0 && (
          <div className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold" style={{ color: 'var(--vs-text)' }}>
                What are you monitoring?
              </h2>
              <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                This decides how the check is run. You can change it later.
              </p>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {KINDS.map((k) => {
                const on = values.type === k.type
                return (
                  <button
                    key={k.type}
                    onClick={() => set('type', k.type)}
                    className="flex items-start gap-3 rounded-lg p-4 text-left transition-colors"
                    style={{
                      border: `1px solid ${on ? 'var(--vs-cyan)' : 'var(--vs-line)'}`,
                      backgroundColor: on ? 'rgba(61,225,255,0.06)' : 'transparent',
                    }}
                  >
                    <k.icon className="mt-0.5 h-5 w-5 shrink-0" style={{ color: on ? 'var(--vs-cyan)' : 'var(--vs-text-dim)' }} />
                    <span>
                      <span className="block text-sm font-semibold" style={{ color: 'var(--vs-text)' }}>
                        {k.title}
                      </span>
                      <span className="block text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                        {k.blurb}
                      </span>
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
        )}

        {/* ---- 2. Target + live test ---- */}
        {step === 1 && (
          <div className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold" style={{ color: 'var(--vs-text)' }}>
                What should we check?
              </h2>
              <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                {kind.hint}
              </p>
            </div>
            <input
              autoFocus
              className={inputCls}
              style={inputStyle}
              value={values.url}
              placeholder={kind.placeholder}
              onChange={(e) => {
                set('url', e.target.value)
                setOutcome(null)
              }}
            />
            {values.type === 'http' && (
              <label className="block">
                <span className="vs-eyebrow mb-1 block">Method</span>
                <select
                  className={inputCls}
                  style={inputStyle}
                  value={values.method}
                  onChange={(e) => {
                    set('method', e.target.value)
                    setOutcome(null)
                  }}
                >
                  <option>GET</option>
                  <option>POST</option>
                  <option>HEAD</option>
                  <option>PUT</option>
                </select>
              </label>
            )}

            <div className="flex flex-wrap items-center gap-3">
              <button
                className="rd-btn rd-btn-secondary"
                disabled={!values.url.trim() || testing}
                onClick={() => void runTest()}
              >
                {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Radio className="h-4 w-4" />}
                {testing ? 'Testing…' : 'Test connection'}
              </button>
              <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                Optional — nothing is saved by testing.
              </span>
            </div>

            {outcome && (
              <div
                className="rounded-lg p-3 text-sm"
                style={{
                  border: `1px solid ${outcome.ok ? 'var(--vs-ecg)' : 'var(--vs-flat)'}`,
                  backgroundColor: outcome.ok ? 'rgba(55,249,138,0.07)' : 'rgba(255,77,77,0.07)',
                  color: outcome.ok ? 'var(--vs-ecg)' : 'var(--vs-flat)',
                }}
              >
                <span className="font-semibold">{outcome.ok ? '✓ Reachable' : '✗ No healthy response'}</span>
                <span className="ml-2 break-all" style={{ color: 'var(--vs-text-dim)' }}>
                  {describeOutcome(outcome)}
                </span>
                {!outcome.ok && (
                  <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                    You can still continue — monitoring something that is currently down is a
                    perfectly good reason to add it.
                  </p>
                )}
              </div>
            )}
          </div>
        )}

        {/* ---- 3. Name + schedule ---- */}
        {step === 2 && (
          <div className="space-y-5">
            <div>
              <h2 className="text-lg font-semibold" style={{ color: 'var(--vs-text)' }}>
                Name it and set the pace
              </h2>
            </div>
            <label className="block">
              <span className="vs-eyebrow mb-1 block">Name</span>
              <input
                autoFocus
                className={inputCls}
                style={inputStyle}
                value={values.name}
                placeholder="API Gateway"
                onChange={(e) => set('name', e.target.value)}
              />
              {errors.name && values.name !== '' && (
                <span className="mt-1 block text-xs" style={{ color: 'var(--vs-flat)' }}>
                  {errors.name}
                </span>
              )}
            </label>

            <div>
              <span className="vs-eyebrow mb-2 block">How often?</span>
              <div className="grid gap-2 sm:grid-cols-2">
                {INTERVAL_PRESETS.map((p) => {
                  const on = values.interval_seconds === p.seconds
                  return (
                    <button
                      key={p.seconds}
                      onClick={() => set('interval_seconds', p.seconds)}
                      className="rounded-lg p-3 text-left transition-colors"
                      style={{
                        border: `1px solid ${on ? 'var(--vs-cyan)' : 'var(--vs-line)'}`,
                        backgroundColor: on ? 'rgba(61,225,255,0.06)' : 'transparent',
                      }}
                    >
                      <span className="block text-sm font-medium" style={{ color: 'var(--vs-text)' }}>
                        {p.label}
                      </span>
                      <span className="block text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                        {p.detail}
                      </span>
                    </button>
                  )
                })}
              </div>
            </div>

            <details className="rounded-lg p-3" style={{ border: '1px solid var(--vs-line)' }}>
              <summary className="cursor-pointer text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                Advanced — timeout and retries
              </summary>
              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <label className="block">
                  <span className="vs-eyebrow mb-1 block">Timeout (seconds)</span>
                  <input
                    type="number"
                    className={inputCls}
                    style={inputStyle}
                    value={values.timeout_seconds}
                    onChange={(e) => set('timeout_seconds', Number(e.target.value))}
                  />
                  {errors.timeout_seconds && (
                    <span className="mt-1 block text-xs" style={{ color: 'var(--vs-flat)' }}>
                      {errors.timeout_seconds}
                    </span>
                  )}
                </label>
                <label className="block">
                  <span className="vs-eyebrow mb-1 block">Retries</span>
                  <input
                    type="number"
                    className={inputCls}
                    style={inputStyle}
                    value={values.retries}
                    onChange={(e) => set('retries', Number(e.target.value))}
                  />
                  {errors.retries && (
                    <span className="mt-1 block text-xs" style={{ color: 'var(--vs-flat)' }}>
                      {errors.retries}
                    </span>
                  )}
                </label>
              </div>
            </details>
          </div>
        )}

        {/* ---- 4. Alerts ---- */}
        {step === 3 && (
          <div className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold" style={{ color: 'var(--vs-text)' }}>
                Where should alerts go?
              </h2>
              <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                Sentinel notifies when this monitor goes down and again when it recovers.
              </p>
            </div>
            <NotificationChannelPicker
              value={values.notify_channels}
              onChange={(v) => set('notify_channels', v)}
            />
          </div>
        )}

        {/* ---- 5. Review ---- */}
        {step === 4 && (
          <div className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold" style={{ color: 'var(--vs-text)' }}>
                Ready to go
              </h2>
            </div>
            <dl className="space-y-2 text-sm">
              {[
                ['Name', values.name || '—'],
                ['Kind', kind.title],
                ['Target', values.url || '—'],
                ['Checked', INTERVAL_PRESETS.find((p) => p.seconds === values.interval_seconds)?.label ?? `Every ${values.interval_seconds}s`],
                ['Timeout', `${values.timeout_seconds}s, ${values.retries} retries`],
                [
                  'Alerts',
                  values.notify_channels == null
                    ? 'All configured channels'
                    : values.notify_channels.length === 0
                      ? 'None — silent monitoring'
                      : values.notify_channels.join(', '),
                ],
              ].map(([k, v]) => (
                <div key={k} className="flex justify-between gap-4 border-b pb-2" style={{ borderColor: 'var(--vs-line)' }}>
                  <dt style={{ color: 'var(--vs-text-dim)' }}>{k}</dt>
                  <dd className="text-right" style={{ color: 'var(--vs-text)' }}>
                    {v}
                  </dd>
                </div>
              ))}
            </dl>

            <div className="grid gap-3 sm:grid-cols-2">
              <label className="block">
                <span className="vs-eyebrow mb-1 block">Tags (optional)</span>
                <input
                  className={inputCls}
                  style={inputStyle}
                  value={values.tags}
                  placeholder="production, critical"
                  onChange={(e) => set('tags', e.target.value)}
                />
              </label>
              <label className="block">
                <span className="vs-eyebrow mb-1 block">Group (optional)</span>
                <select className={inputCls} style={inputStyle} value={groupId} onChange={(e) => setGroupId(e.target.value)}>
                  <option value="">Ungrouped</option>
                  {groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            {Object.keys(errors).length > 0 && (
              <div className="rounded-lg p-3 text-sm" style={{ border: '1px solid var(--vs-flat)', color: 'var(--vs-flat)' }}>
                {Object.values(errors)[0]}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Footer navigation */}
      <div className="flex items-center justify-between gap-3">
        <button className="rd-btn rd-btn-secondary" onClick={back} disabled={step === 0}>
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
        {step < 4 ? (
          <button className="rd-btn rd-btn-primary" onClick={next} disabled={!canAdvance[step]}>
            Continue <ArrowRight className="h-4 w-4" />
          </button>
        ) : (
          <button className="rd-btn rd-btn-primary" onClick={() => void submit()} disabled={creating || !canAdvance[4]}>
            {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
            {creating ? 'Creating…' : 'Create monitor'}
          </button>
        )}
      </div>

      <Toaster toasts={toasts} />
    </div>
  )
}
