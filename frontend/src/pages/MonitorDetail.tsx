import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { format } from 'date-fns'
import {
  ArrowLeft,
  Pencil,
  Play,
  Pause,
  Trash2,
  ExternalLink,
  Wrench,
  Share2,
  Activity,
  AlertTriangle,
  FileText,
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
import { useMonitorUptime, type UptimeRange } from '@/hooks/useMonitorUptime'
import { useMonitorGroups, useMoveMonitorToGroup } from '@/hooks/useMonitorGroups'
import { useAppConfig } from '@/context/AppConfigContext'
import { useUsers } from '@/hooks/useUsers'
import { monitorAccess } from '@/utils/monitorAccess'
import {
  useGetMaintenanceStatus,
  useEnableMaintenanceMode,
  useUpdateMaintenanceWindow,
  useDisableMaintenanceMode,
} from '@/hooks/useMaintenanceMode'
import MonitorForm, { monitorToForm } from '@/components/MonitorForm'
import TestResult from '@/components/TestResult'
import ShareModal from '@/components/ShareModal'
import GenerateReportModal from '@/components/GenerateReportModal'
import IncidentList from '@/components/IncidentList'
import { useToasts, Toaster } from '@/components/Toast'
import {
  formatDatetime,
  formatResponseTime,
  formatLastResponseTime,
  getStatusBgColor,
} from '@/utils/formatters'
import type { Check, MonitorInput } from '@/types'
import MonitorTypeBadge from '@/components/MonitorTypeBadge'
import MonitorPerformance from '@/components/MonitorPerformance'
import { Sparkline, hourlySparklinePoints, STATUS_COLOR, uptimeColor as windowUptimeColor } from '@/components/UptimeSparkline'

// Format a Date for a datetime-local input (local time, minute precision).
function toLocalInput(d: Date): string {
  return format(d, "yyyy-MM-dd'T'HH:mm")
}

type Mode = 'view' | 'edit' | 'create'

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-2 text-sm">
      <span className="text-slate-400">{label}</span>
      <span className="text-right font-medium">{children}</span>
    </div>
  )
}

function UptimeBox({ label, pct }: { label: string; pct: number | undefined }) {
  return (
    <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4 text-center backdrop-blur-sm">
      <div className={`text-2xl font-light ${pct != null ? windowUptimeColor(pct) : 'text-slate-400'}`}>
        {pct != null ? `${pct.toFixed(2)}%` : '—'}
      </div>
      <div className="text-xs text-slate-400">{label}</div>
    </div>
  )
}

function StatBox({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4 text-center backdrop-blur-sm">
      <div className={`text-2xl font-light text-white ${tone ?? ''}`}>{value}</div>
      <div className="text-xs text-slate-400">{label}</div>
    </div>
  )
}

// Only ever render a monitor URL as a clickable link it it's actually
// http(s). Guards against a stored "javascript:" URL executing on click,
// even if it somehow slipped past server-side validation
function isSafeHttpUrl(value: string): boolean {
	try {
		const u = new URL(value)
		return u.protocol === 'http:' || u.protocol === 'https:'
	} catch {
		return false
	}
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

  // The uptime series drives the health bar, the three windows and the chart.
  // Only the chart follows the range; the bar is a 24-hour view by definition.
  const [uptimeRange, setUptimeRange] = useState<UptimeRange>('24h')
  const { data: uptime, loading: uptimeLoading } = useMonitorUptime(
    id ?? '',
    uptimeRange,
    mode === 'view' && !!id,
  )
  const { groups } = useMonitorGroups()
  const { move } = useMoveMonitorToGroup()
  const { appName } = useAppConfig()

  const { create, loading: creating, error: createErr } = useCreateMonitor()
  const { update, loading: updating, error: updateErr } = useUpdateMonitor(id)
  const { delete: deleteMonitor } = useDeleteMonitor(id)
  const { pause } = usePauseMonitor(id)
  const { resume } = useResumeMonitor(id)
  const { usernameFor } = useUsers()
  const { test } = useTestMonitor(id)

  // Maintenance mode.
  const { status: maint, refetch: refetchMaint } = useGetMaintenanceStatus(mode === 'view' ? id : undefined)
  const { enable: enableMaint } = useEnableMaintenanceMode()
  const { update: updateMaint } = useUpdateMaintenanceWindow()
  const { disable: disableMaint } = useDisableMaintenanceMode()

  // The tab is how someone finds this page again among several open monitors,
  // so it carries the monitor's name rather than the app's alone.
  useEffect(() => {
    if (mode !== 'view' || !monitor) return
    const previous = document.title
    document.title = `${monitor.name} · ${appName}`
    return () => {
      document.title = previous
    }
  }, [mode, monitor, appName])

  // The monitor is read once and has no refetch, so the group control tracks
  // its own value after a move rather than showing a stale one.
  const [groupOverride, setGroupOverride] = useState<string | null | undefined>(undefined)
  const currentGroupID = groupOverride !== undefined ? groupOverride : (monitor?.group_id ?? null)
  const handleMoveGroup = async (gid: string | null) => {
    if (!id) return
    const previous = currentGroupID
    setGroupOverride(gid)
    try {
      await move(id, gid)
      push('Monitor group updated', 'success')
    } catch (err) {
      setGroupOverride(previous)
      push((err as { message?: string }).message ?? 'Could not move the monitor', 'error')
    }
  }

  const [testCheck, setTestCheck] = useState<Check | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)
  const [reportOpen, setReportOpen] = useState(false)
  const [maintModal, setMaintModal] = useState(false)
  const [maintStart, setMaintStart] = useState('')
  const [maintEnd, setMaintEnd] = useState('')

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
        <button className="btn-secondary" onClick={() => navigate('/uptime')}>
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
        <h1 className="vs-title text-2xl">Create Monitor</h1>
        <MonitorForm
          onSubmit={handleCreate}
          isLoading={creating}
          error={createErr}
          submitLabel="Create Monitor"
          onCancel={() => navigate('/uptime')}
        />
        <Toaster toasts={toasts} />
      </div>
    )
  }

  if (mode === 'edit') {
    if (loading || !monitor) {
      return <div className="text-slate-500">Loading…</div>
    }
    return (
      <div className="max-w-3xl space-y-6">
        <button className="btn-secondary" onClick={() => navigate(`/monitors/${id}`)}>
          <ArrowLeft className="h-4 w-4" /> Back
        </button>
        <h1 className="vs-title text-2xl">Edit Monitor</h1>
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
  if (loading && !monitor) return <div className="text-slate-500">Loading…</div>
  if (!monitor) {
    return (
      <div className="space-y-4">
        <div className="card p-6 text-slate-500">Monitor not found.</div>
        <button className="btn-secondary" onClick={() => navigate('/uptime')}>
          <ArrowLeft className="h-4 w-4" /> Back to monitors
        </button>
      </div>
    )
  }

  const isHttp = monitor.type === 'http'
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const access = monitorAccess(monitor)
  const ownerUsername = usernameFor(monitor.owner_id)

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
      navigate('/uptime')
    } catch {
      push('Delete failed', 'error')
      setConfirmDelete(false)
    }
  }

  const openMaintModal = () => {
    const now = new Date()
    const start = maint?.start_time ? new Date(maint.start_time) : now
    const end = maint?.end_time ? new Date(maint.end_time) : new Date(now.getTime() + 2 * 3600_000)
    setMaintStart(toLocalInput(start))
    setMaintEnd(toLocalInput(end))
    setMaintModal(true)
  }
  const applyPreset = (hours: number) => {
    const now = new Date()
    setMaintStart(toLocalInput(now))
    setMaintEnd(toLocalInput(new Date(now.getTime() + hours * 3600_000)))
  }
  const submitMaint = async () => {
    if (!id) return
    const startISO = new Date(maintStart).toISOString()
    const endISO = new Date(maintEnd).toISOString()
    try {
      if (maint?.enabled) {
        await updateMaint(id, startISO, endISO)
        push('Maintenance window updated', 'success')
      } else {
        await enableMaint(id, startISO, endISO)
        push('Maintenance mode enabled', 'success')
      }
      setMaintModal(false)
      await refetchMaint()
    } catch (err) {
      push((err as { message?: string }).message || 'Failed to save maintenance window', 'error')
    }
  }
  const endMaintNow = async () => {
    if (!id) return
    try {
      await disableMaint(id)
      push('Maintenance mode disabled', 'success')
      await refetchMaint()
    } catch {
      push('Failed to disable maintenance', 'error')
    }
  }

  return (
    <div className="space-y-6">
      <Toaster toasts={toasts} />

      {/* Back link and breadcrumb, matching a server's page: the list is one
          click away and the trail says which list this came from. */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link
            to="/uptime"
            className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" /> Back to Monitors
          </Link>
          <nav aria-label="Breadcrumb" className="mt-1 text-xs text-slate-600">
            <Link to="/uptime" className="transition hover:text-slate-400">
              Uptime Monitoring
            </Link>
            <span className="px-1">›</span>
            <span className="text-slate-500">{monitor.name}</span>
          </nav>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button className="btn-secondary !py-1.5" onClick={() => setReportOpen(true)}>
            <FileText className="h-4 w-4" /> Generate Report
          </button>
          {(access.isOwner || access.permission === 'admin') && (
            <button className="btn-secondary !py-1.5" onClick={() => setShareOpen(true)}>
              <Share2 className="h-4 w-4" /> Share
            </button>
          )}
          <button className="btn-secondary !py-1.5" onClick={() => void handleTest()}>
            <Play className="h-4 w-4" /> Test
          </button>
          {access.canEdit && (
            <button className="btn-secondary !py-1.5" onClick={() => void handlePauseResume()}>
              {monitor.enabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
              {monitor.enabled ? 'Pause' : 'Resume'}
            </button>
          )}
          {access.canEdit && (
            <button className="btn-secondary !py-1.5" onClick={() => navigate(`/monitors/${id}/edit`)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
          )}
          {access.canDelete && (
            <button
              className="rounded-lg border border-red-500/30 px-3 py-1.5 text-sm text-red-400 transition hover:bg-red-500/10"
              aria-label={`Delete ${monitor.name}`}
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="inline h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      {/* The configuration sits after the metrics on narrow screens: the
          numbers are what someone came for, the settings are reference. */}
      <div className="grid gap-6 xl:grid-cols-[minmax(0,3fr)_minmax(280px,1fr)]">
        <div className="min-w-0 space-y-6">
          <div className="flex items-start gap-3">
            <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-slate-800/60 text-slate-300">
              <Activity className="h-5 w-5" aria-hidden />
            </span>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-2xl font-light text-white">{monitor.name}</h1>
                <MonitorTypeBadge type={monitor.type} />
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-2">
                <span
                  className={`inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium ${getStatusBgColor(
                    monitor.current_status,
                  )}`}
                >
                  <span
                    className={`h-2 w-2 rounded-full ${
                      online ? 'bg-emerald-500' : offline ? 'animate-pulse bg-red-500' : 'bg-slate-400'
                    }`}
                  />
                  {monitor.current_status}
                  {!monitor.enabled && ' · paused'}
                </span>
                <span className="truncate text-sm text-slate-500">{monitor.url}</span>
              </div>
              <p className="mt-1 text-xs text-slate-500">
                {access.isOwner
                  ? 'Your monitor'
                  : access.permission === 'admin'
                    ? `Owned by ${ownerUsername ?? 'another user'} · admin access`
                    : `Shared with you by ${ownerUsername ?? 'another user'} · ${
                        access.permission === 'editable' ? 'can edit' : 'read-only'
                      }`}
              </p>
            </div>
          </div>

          {!access.canEdit && (
            <div className="rounded-lg border border-white/10 bg-white/5 px-4 py-3 text-sm text-slate-400">
              Read-only access — you can view and test this monitor but not edit or delete it.
            </div>
          )}

          {testCheck && <TestResult check={testCheck} onClose={() => setTestCheck(null)} />}

          {/* Ongoing-downtime banner: the monitor is offline right now. */}
          {report?.metrics.ongoing_incident && (
            <div className="flex items-center gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm font-medium text-red-400">
              <AlertTriangle className="h-4 w-4" aria-hidden />
              Currently Offline
              {report.metrics.current_downtime_minutes > 0 && (
                <span className="font-normal">
                  — down for {report.metrics.current_downtime_minutes.toFixed(1)} min
                </span>
              )}
            </div>
          )}

          {/* Uptime over the three windows the reports use, so this page and a
              report never disagree about the same monitor. */}
          <div className="grid grid-cols-3 gap-3">
            <UptimeBox label="24-hour" pct={uptime?.uptime_24h} />
            <UptimeBox label="7-day" pct={uptime?.uptime_7d} />
            <UptimeBox label="30-day" pct={uptime?.uptime_30d} />
          </div>

          <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
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

          {/* The health bar is a 24-hour view by definition and does not follow
              the chart's range selector, so it is labelled with its own window. */}
          <section>
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2 text-xs font-medium text-slate-400">
              <span>Uptime (last 24 hours)</span>
              <span className="flex items-center gap-3">
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.up }} /> up
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.partial }} />{' '}
                  partial
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.down }} /> down
                </span>
              </span>
            </div>
            <div className="rounded-lg border border-white/10 bg-slate-800/40 p-3 backdrop-blur-sm">
              {uptime ? (
                <Sparkline data={hourlySparklinePoints(uptime.hourly_data)} className="h-12" />
              ) : (
                <div className="flex h-12 items-center text-xs text-slate-400">
                  {uptimeLoading ? 'Loading…' : 'No data'}
                </div>
              )}
            </div>
          </section>

          <MonitorPerformance
            uptime={uptime}
            loading={uptimeLoading}
            range={uptimeRange}
            onRangeChange={setUptimeRange}
          />

          {/* Maintenance mode */}
          <section>
            <div className="mb-3 flex items-center justify-between">
              <h2 className="flex items-center gap-2 text-lg font-light text-white">
                <Wrench className="h-4 w-4" /> Maintenance Mode
              </h2>
              {maint?.enabled ? (
                <span className="rounded-md bg-amber-500/20 px-2 py-1 text-xs font-medium text-amber-300">
                  {maint.status === 'active'
                    ? 'Active'
                    : maint.status === 'scheduled'
                      ? 'Scheduled'
                      : 'Expired'}
                </span>
              ) : (
                <span className="rounded-md bg-white/5 px-2 py-1 text-xs font-medium text-slate-400">
                  Not in maintenance
                </span>
              )}
            </div>

            <div className="rounded-lg border border-white/10 bg-slate-800/40 p-5 backdrop-blur-sm">
              {maint?.enabled ? (
                <div className="space-y-3 text-sm">
                  <div className="flex justify-between">
                    <span className="text-slate-500">Start</span>
                    <span className="font-medium text-slate-200">
                      {maint.start_time ? formatDatetime(maint.start_time) : '—'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-500">End</span>
                    <span className="font-medium text-slate-200">
                      {maint.end_time ? formatDatetime(maint.end_time) : '—'}
                    </span>
                  </div>
                  {maint.is_currently_in_maintenance && (
                    <div className="rounded-md bg-amber-500/10 p-3 text-center font-medium text-amber-300">
                      Ends in {maint.time_remaining_minutes} minute
                      {maint.time_remaining_minutes === 1 ? '' : 's'}
                      <div className="mt-1 text-xs font-normal">
                        No incidents will be created during maintenance.
                      </div>
                    </div>
                  )}
                  {access.canEdit && (
                    <div className="flex gap-2">
                      <button className="btn-secondary !py-1.5" onClick={openMaintModal}>
                        <Pencil className="h-4 w-4" /> Edit Window
                      </button>
                      <button
                        className="rounded-lg border border-red-500/30 px-3 py-1.5 text-sm text-red-400 transition hover:bg-red-500/10"
                        onClick={() => void endMaintNow()}
                      >
                        End Now
                      </button>
                    </div>
                  )}
                </div>
              ) : (
                <div className="space-y-3">
                  <p className="text-sm text-slate-400">
                    Schedule a window during which failed checks won&rsquo;t create incidents or send
                    alerts.
                  </p>
                  {access.canEdit && (
                    <button className="btn-primary !py-1.5" onClick={openMaintModal}>
                      <Wrench className="h-4 w-4" /> Enable Maintenance Mode
                    </button>
                  )}
                </div>
              )}
            </div>
          </section>

          <section>
            <h2 className="mb-3 text-lg font-light text-white">Recent Incidents</h2>
            <div className="rounded-lg border border-white/10 bg-slate-800/40 p-5 backdrop-blur-sm">
              <IncidentList monitorId={monitor.id} />
            </div>
          </section>
        </div>

        <aside className="space-y-4 xl:sticky xl:top-6 xl:self-start">
          <div className="rounded-lg border border-white/10 bg-slate-800/40 p-5 backdrop-blur-sm">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Configuration
            </h2>
            <div className="divide-y divide-white/5">
              <DetailRow label="URL / Target">
                {isHttp && isSafeHttpUrl(monitor.url) ? (
                  <a
                    href={monitor.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 break-all text-primary-400 hover:underline"
                  >
                    {monitor.url} <ExternalLink className="h-3 w-3 shrink-0" />
                  </a>
                ) : (
                  <span className="break-all">{monitor.url}</span>
                )}
              </DetailRow>
              {isHttp && <DetailRow label="Method">{monitor.method || 'GET'}</DetailRow>}
              <DetailRow label="Last check">
                {monitor.last_check_at ? formatDatetime(monitor.last_check_at) : 'Never'}
              </DetailRow>
              <DetailRow label="Last response">
                {formatLastResponseTime(monitor.last_response_time_ms, monitor.current_status)}
              </DetailRow>
              <DetailRow label="Interval">{monitor.interval_seconds}s</DetailRow>
              <DetailRow label="Timeout">{monitor.timeout_seconds}s</DetailRow>
              <DetailRow label="Retries">{monitor.retries}</DetailRow>
              {/* Spelled out because the number alone reads as a duplicate of
                  Retries, which it is not. */}
              <DetailRow label="Failures before incident">
                {monitor.failure_threshold ?? 2}
                <span className="ml-1 text-xs font-normal text-slate-500">
                  consecutive
                </span>
              </DetailRow>
            </div>

            {/* Group assignment lives here now that a row opens this page
                instead of a drawer. */}
            {access.canEdit && (
              <label className="mt-4 flex items-center justify-between gap-2 text-sm">
                <span className="text-slate-400">Group</span>
                <select
                  className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-sm text-white"
                  value={currentGroupID ?? ''}
                  onChange={(e) => void handleMoveGroup(e.target.value || null)}
                >
                  <option value="">Ungrouped</option>
                  {groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              </label>
            )}

            {monitor.tags && monitor.tags.length > 0 && (
              <div className="mt-4">
                <div className="mb-1 text-xs text-slate-500">Tags</div>
                <div className="flex flex-wrap gap-1">
                  {monitor.tags.map((t) => (
                    <span
                      key={t}
                      className="rounded-md bg-primary-500/20 px-2 py-0.5 text-xs text-primary-300"
                    >
                      {t}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {isHttp && monitor.headers && Object.keys(monitor.headers).length > 0 && (
              <div className="mt-4">
                <div className="mb-1 text-xs text-slate-500">Headers</div>
                <pre className="overflow-x-auto rounded-md bg-white/5 p-3 text-xs text-slate-300">
                  {JSON.stringify(monitor.headers, null, 2)}
                </pre>
              </div>
            )}
            {isHttp && monitor.body && (
              <div className="mt-4">
                <div className="mb-1 text-xs text-slate-500">Body</div>
                <pre className="overflow-x-auto rounded-md bg-white/5 p-3 text-xs text-slate-300">
                  {monitor.body}
                </pre>
              </div>
            )}
          </div>
        </aside>
      </div>

      {/* Maintenance window modal */}
      {maintModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md space-y-4 rounded-xl border border-white/10 bg-slate-900/95 p-6">
            <h3 className="text-lg font-semibold text-white">
              {maint?.enabled ? 'Edit maintenance window' : 'Enable maintenance mode'}
            </h3>
            <div className="flex flex-wrap gap-2">
              {[1, 2, 4].map((h) => (
                <button
                  key={h}
                  type="button"
                  className="btn-secondary !py-1"
                  onClick={() => applyPreset(h)}
                >
                  {h} hour{h > 1 ? 's' : ''}
                </button>
              ))}
            </div>
            <label className="block">
              <span className="mb-1 block text-sm font-medium text-white">Start</span>
              <input
                type="datetime-local"
                value={maintStart}
                onChange={(e) => setMaintStart(e.target.value)}
                className="w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-sm font-medium text-white">End</span>
              <input
                type="datetime-local"
                value={maintEnd}
                onChange={(e) => setMaintEnd(e.target.value)}
                className="w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500"
              />
            </label>
            {maintStart && maintEnd && new Date(maintEnd) <= new Date(maintStart) && (
              <p className="text-xs text-error-600">End must be after start.</p>
            )}
            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setMaintModal(false)}>
                Cancel
              </button>
              <button
                className="btn-primary"
                disabled={!maintStart || !maintEnd || new Date(maintEnd) <= new Date(maintStart)}
                onClick={() => void submitMaint()}
              >
                {maint?.enabled ? 'Save' : 'Enable'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirmation */}
      {confirmDelete && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onMouseDown={(e) => e.target === e.currentTarget && setConfirmDelete(false)}
        >
          <div
            role="dialog"
            aria-modal="true"
            className="w-full max-w-sm rounded-xl border border-white/10 bg-slate-900/95 p-6"
          >
            <h3 className="text-lg font-semibold text-white">Delete monitor?</h3>
            <p className="mt-2 text-sm text-slate-400">
              This permanently deletes {monitor.name} and all its history.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>
                Cancel
              </button>
              <button
                className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500"
                onClick={() => void handleDelete()}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}

      {shareOpen && id && (
        <ShareModal monitorId={id} onClose={() => setShareOpen(false)} push={push} />
      )}

      <GenerateReportModal
        isOpen={reportOpen}
        onClose={() => setReportOpen(false)}
        fixedScope={{ scope_type: 'monitors', ids: [monitor.id], label: monitor.name }}
      />
    </div>
  )
}
