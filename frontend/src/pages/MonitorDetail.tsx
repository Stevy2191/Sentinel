import { useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  Pencil,
  Play,
  Pause,
  Trash2,
  ExternalLink,
} from 'lucide-react'
import {
  useMonitor,
  useCreateMonitor,
  useUpdateMonitor,
  useDeleteMonitor,
  usePauseMonitor,
  useResumeMonitor,
  useTestMonitor,
} from '@/hooks/useMonitors'
import { useUptimeReport } from '@/hooks/useReports'
import MonitorForm, { monitorToForm } from '@/components/MonitorForm'
import TestResult from '@/components/TestResult'
import IncidentList from '@/components/IncidentList'
import { useToasts, Toaster } from '@/components/Toast'
import {
  formatDatetime,
  formatResponseTime,
  getStatusBgColor,
} from '@/utils/formatters'
import type { Check, MonitorInput } from '@/types'

type Mode = 'view' | 'edit' | 'create'

function uptimeColor(pct: number): string {
  if (pct < 90) return 'text-red-500'
  if (pct < 99) return 'text-amber-500'
  return 'text-emerald-500'
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-2 text-sm">
      <span className="text-neutral-500 dark:text-neutral-400">{label}</span>
      <span className="text-right font-medium">{children}</span>
    </div>
  )
}

function StatBox({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div className="card p-4 text-center">
      <div className={`text-2xl font-bold ${tone ?? ''}`}>{value}</div>
      <div className="text-xs text-neutral-500 dark:text-neutral-400">{label}</div>
    </div>
  )
}

export default function MonitorDetail({ mode }: { mode: Mode }) {
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  const { toasts, push } = useToasts()

  const { monitor, loading } = useMonitor(mode === 'create' ? undefined : id)

  // Stats window: last 24 hours (memoized so the report hook doesn't refetch).
  const range = useMemo(() => {
    const end = new Date()
    const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
    return { start: start.toISOString(), end: end.toISOString() }
  }, [])
  const { report } = useUptimeReport(
    mode === 'view' ? id : undefined,
    range.start,
    range.end
  )

  const { create, loading: creating, error: createErr } = useCreateMonitor()
  const { update, loading: updating, error: updateErr } = useUpdateMonitor(id)
  const { delete: deleteMonitor } = useDeleteMonitor(id)
  const { pause } = usePauseMonitor(id)
  const { resume } = useResumeMonitor(id)
  const { test } = useTestMonitor(id)

  const [testCheck, setTestCheck] = useState<Check | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)

  // ---- Create / Edit forms ----
  const handleCreate = async (input: MonitorInput) => {
    const created = await create(input)
    push('Monitor created', 'success')
    navigate(`/monitors/${created.id}`)
  }
  const handleUpdate = async (input: MonitorInput) => {
    await update(input)
    push('Monitor updated', 'success')
    navigate(`/monitors/${id}`)
  }

  if (mode === 'create') {
    return (
      <div className="max-w-3xl space-y-6">
        <button className="btn-secondary" onClick={() => navigate('/monitors')}>
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
        <h1 className="text-2xl font-bold">Create Monitor</h1>
        <MonitorForm
          onSubmit={handleCreate}
          isLoading={creating}
          error={createErr}
          submitLabel="Create Monitor"
          onCancel={() => navigate('/monitors')}
        />
        <Toaster toasts={toasts} />
      </div>
    )
  }

  if (mode === 'edit') {
    if (loading || !monitor) {
      return <div className="text-neutral-500">Loading…</div>
    }
    return (
      <div className="max-w-3xl space-y-6">
        <button className="btn-secondary" onClick={() => navigate(`/monitors/${id}`)}>
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
        <h1 className="text-2xl font-bold">Edit Monitor</h1>
        <MonitorForm
          initialValues={monitorToForm(monitor)}
          onSubmit={handleUpdate}
          isLoading={updating}
          error={updateErr}
          submitLabel="Update Monitor"
          onCancel={() => navigate(`/monitors/${id}`)}
        />
        <Toaster toasts={toasts} />
      </div>
    )
  }

  // ---- View ----
  if (loading && !monitor) return <div className="text-neutral-500">Loading…</div>
  if (!monitor) {
    return (
      <div className="space-y-4">
        <div className="card p-6 text-neutral-500">Monitor not found.</div>
        <button className="btn-secondary" onClick={() => navigate('/monitors')}>
          <ArrowLeft className="h-4 w-4" /> Back to monitors
        </button>
      </div>
    )
  }

  const isHttp = monitor.type === 'http'
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'

  const handleTest = async () => {
    try {
      const check = await test()
      setTestCheck(check)
      push(`Test: ${check.status}`, check.status === 'success' ? 'success' : 'error')
    } catch {
      push('Test failed', 'error')
    }
  }
  const handlePauseResume = async () => {
    try {
      if (monitor.enabled) {
        await pause()
        push('Monitor paused (updates shortly)', 'info')
      } else {
        await resume()
        push('Monitor resumed (updates shortly)', 'success')
      }
    } catch {
      push('Action failed', 'error')
    }
  }
  const handleDelete = async () => {
    try {
      await deleteMonitor()
      navigate('/monitors')
    } catch {
      push('Delete failed', 'error')
      setConfirmDelete(false)
    }
  }

  return (
    <div className="max-w-4xl space-y-6">
      <button className="btn-secondary" onClick={() => navigate('/monitors')}>
        <ArrowLeft className="h-4 w-4" /> Back
      </button>

      {/* Header + actions */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold">{monitor.name}</h1>
            <span className="rounded-md bg-neutral-100 px-2 py-1 text-xs font-semibold uppercase text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400">
              {monitor.type}
            </span>
          </div>
          <span
            className={`mt-2 inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium ${getStatusBgColor(
              monitor.current_status
            )}`}
          >
            <span
              className={`h-2 w-2 rounded-full ${
                online ? 'bg-emerald-500' : offline ? 'bg-red-500 animate-pulse' : 'bg-neutral-400'
              }`}
            />
            {monitor.current_status}
            {!monitor.enabled && ' · paused'}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn-secondary" onClick={() => navigate(`/monitors/${id}/edit`)}>
            <Pencil className="h-4 w-4" /> Edit
          </button>
          <button className="btn-secondary" onClick={() => void handleTest()}>
            <Play className="h-4 w-4" /> Test
          </button>
          <button className="btn-secondary" onClick={() => void handlePauseResume()}>
            {monitor.enabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
            {monitor.enabled ? 'Pause' : 'Resume'}
          </button>
          <button
            className="btn border border-error-300 text-error-600 hover:bg-error-50 dark:hover:bg-error-900/20"
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 className="h-4 w-4" /> Delete
          </button>
        </div>
      </div>

      {testCheck && <TestResult check={testCheck} onClose={() => setTestCheck(null)} />}

      {/* Stats (last 24h) */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatBox
          label="Uptime (24h)"
          value={report ? `${report.metrics.uptime_percentage.toFixed(2)}%` : '—'}
          tone={report ? uptimeColor(report.metrics.uptime_percentage) : ''}
        />
        <StatBox
          label="Avg Response (24h)"
          value={report ? formatResponseTime(report.metrics.avg_response_time_ms) : '—'}
        />
        <StatBox label="Checks (24h)" value={report ? String(report.metrics.total_checks) : '—'} />
        <StatBox
          label="Failed (24h)"
          value={report ? String(report.metrics.failed_checks) : '—'}
          tone={report && report.metrics.failed_checks > 0 ? 'text-red-500' : ''}
        />
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* Configuration */}
        <div className="card p-5">
          <h2 className="mb-2 font-semibold">Configuration</h2>
          <div className="divide-y divide-neutral-100 dark:divide-neutral-800">
            <DetailRow label="URL / Target">
              {isHttp ? (
                <a
                  href={monitor.url}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 text-primary-600 hover:underline"
                >
                  {monitor.url} <ExternalLink className="h-3 w-3" />
                </a>
              ) : (
                monitor.url
              )}
            </DetailRow>
            {isHttp && <DetailRow label="Method">{monitor.method || 'GET'}</DetailRow>}
            <DetailRow label="Last check">
              {monitor.last_check_at ? formatDatetime(monitor.last_check_at) : 'Never'}
            </DetailRow>
            <DetailRow label="Last response">
              {formatResponseTime(monitor.last_response_time_ms)}
            </DetailRow>
            <DetailRow label="Interval">{monitor.interval_seconds}s</DetailRow>
            <DetailRow label="Timeout">{monitor.timeout_seconds}s</DetailRow>
            <DetailRow label="Retries">{monitor.retries}</DetailRow>
          </div>

          {monitor.tags && monitor.tags.length > 0 && (
            <div className="mt-4">
              <div className="mb-1 text-sm text-neutral-500">Tags</div>
              <div className="flex flex-wrap gap-1">
                {monitor.tags.map((t) => (
                  <span
                    key={t}
                    className="rounded-md bg-primary-100 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
                  >
                    {t}
                  </span>
                ))}
              </div>
            </div>
          )}

          {isHttp && monitor.headers && Object.keys(monitor.headers).length > 0 && (
            <div className="mt-4">
              <div className="mb-1 text-sm text-neutral-500">Headers</div>
              <pre className="overflow-x-auto rounded-md bg-neutral-100 p-3 text-xs dark:bg-neutral-800">
                {JSON.stringify(monitor.headers, null, 2)}
              </pre>
            </div>
          )}
          {isHttp && monitor.body && (
            <div className="mt-4">
              <div className="mb-1 text-sm text-neutral-500">Body</div>
              <pre className="overflow-x-auto rounded-md bg-neutral-100 p-3 text-xs dark:bg-neutral-800">
                {monitor.body}
              </pre>
            </div>
          )}
        </div>

        {/* Incidents */}
        <div className="card p-5">
          <h2 className="mb-2 font-semibold">Recent Incidents</h2>
          <IncidentList monitorId={monitor.id} />
        </div>
      </div>

      {/* Delete confirmation */}
      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm p-6">
            <h3 className="text-lg font-semibold">Delete monitor?</h3>
            <p className="mt-2 text-sm text-neutral-500 dark:text-neutral-400">
              This permanently deletes {monitor.name} and all its history.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>
                Cancel
              </button>
              <button
                className="btn bg-error-600 text-white hover:bg-error-700"
                onClick={() => void handleDelete()}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
