import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Pause, Pencil, Trash2, Loader2, AlertTriangle, Wrench } from 'lucide-react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import api from '@/services/api'
import {
  usePauseMonitor,
  useResumeMonitor,
  useDeleteMonitor,
  useTestMonitor,
} from '@/hooks/useMonitors'
import { useMoveMonitorToGroup } from '@/hooks/useMonitorGroups'
import { useEnableMaintenanceMode, useDisableMaintenanceMode } from '@/hooks/useMaintenanceMode'
import ActionMenu, { type ActionItem } from '@/components/ActionMenu'
import { formatDatetime } from '@/utils/formatters'
import type { HourPoint, HourStatus, UptimeHistory } from '@/hooks/useMonitorUptime'
import type { MonitorAccess } from '@/utils/monitorAccess'
import type { Monitor, MonitorGroup } from '@/types'

export function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-primary-400'
  if (pct >= 80) return 'text-amber-400'
  return 'text-red-400'
}

const STATUS_COLOR: Record<HourStatus, string> = {
  up: '#10b981', // ECG green
  down: '#ef4444', // flatline red
  partial: '#eab308', // amber
  nodata: '#4E5E68', // dim
}

// Sparkline renders 24 hourly bars colored by status. Bar height reflects the
// hour's uptime (with a floor so down/no-data hours stay visible). Native title
// tooltips show the hour + status. Shared by the card and the detail panel.
export function Sparkline({ data, className }: { data: HourPoint[]; className?: string }) {
  const height = (d: HourPoint) => (d.status === 'nodata' ? 15 : Math.max(12, d.uptime))
  return (
    <div className={`flex items-end gap-px ${className ?? ''}`}>
      {data.map((d, i) => (
        <div
          key={i}
          title={`${String(d.hour).padStart(2, '0')}:00 — ${d.status}${d.status === 'nodata' ? '' : ` (${d.uptime}%)`}`}
          className="flex-1 rounded-sm"
          style={{ height: `${height(d)}%`, minWidth: 2, backgroundColor: STATUS_COLOR[d.status] }}
        />
      ))}
    </div>
  )
}

interface IncidentRow {
  id: string
  start_time: string
  end_time: string | null
  duration_seconds: number
}

// severity is derived from downtime length (or ongoing).
function severityOf(inc: IncidentRow): { label: string; cls: string } {
  if (inc.end_time === null || inc.duration_seconds >= 3600)
    return { label: 'high', cls: 'bg-red-500/20 text-red-400' }
  if (inc.duration_seconds >= 900)
    return { label: 'medium', cls: 'bg-amber-500/20 text-amber-400' }
  return { label: 'low', cls: 'bg-white/5 text-slate-400' }
}

function UptimeBox({ label, pct }: { label: string; pct: number | undefined }) {
  return (
    <div className="rounded-lg bg-white/5 p-3 text-center">
      <div className={`text-2xl font-bold ${pct != null ? uptimeColor(pct) : 'text-slate-400'}`}>
        {pct != null ? `${pct.toFixed(2)}%` : '—'}
      </div>
      <div className="text-xs text-slate-400">{label}</div>
    </div>
  )
}

interface Props {
  monitor: Monitor
  uptime: UptimeHistory | null
  uptimeLoading: boolean
  groups: MonitorGroup[]
  access: MonitorAccess
  ownerUsername?: string
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

export default function DetailPanel({ monitor, uptime, uptimeLoading, groups, access, ownerUsername, onChanged, push }: Props) {
  const navigate = useNavigate()
  const { pause, loading: pausing } = usePauseMonitor(monitor.id)
  const { resume, loading: resuming } = useResumeMonitor(monitor.id)
  const { delete: del, loading: deleting } = useDeleteMonitor(monitor.id)
  const { test, loading: testing } = useTestMonitor(monitor.id)
  const { move } = useMoveMonitorToGroup()
  const { enable, loading: enablingMaint } = useEnableMaintenanceMode()
  const { disable, loading: disablingMaint } = useDisableMaintenanceMode()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [maintOpen, setMaintOpen] = useState(false)
  const [customUntil, setCustomUntil] = useState('')
  const busy = pausing || resuming || deleting || testing || enablingMaint || disablingMaint

  // Incidents load lazily — the panel only mounts when the card is expanded.
  const [incidents, setIncidents] = useState<IncidentRow[] | null>(null)
  useEffect(() => {
    let active = true
    const now = new Date()
    const start = new Date(now.getTime() - 30 * 864e5)
    api
      .get<{ data: { incidents: IncidentRow[] } }>(`/monitors/${monitor.id}/incidents`, {
        params: { start_time: start.toISOString(), end_time: now.toISOString() },
      })
      .then((r) => active && setIncidents(r.data.data.incidents ?? []))
      .catch(() => active && setIncidents([]))
    return () => {
      active = false
    }
  }, [monitor.id])

  const act = async (fn: () => Promise<unknown>, okMsg: string) => {
    try {
      await fn()
      push(okMsg, 'success')
      onChanged()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Action failed', 'error')
    }
  }

  // Put the monitor into maintenance starting now for the given number of hours.
  const startMaintenance = (hours: number) => {
    const start = new Date()
    const end = new Date(start.getTime() + hours * 3600e3)
    void act(async () => {
      await enable(monitor.id, start.toISOString(), end.toISOString())
      setMaintOpen(false)
    }, 'Maintenance started')
  }
  const startMaintenanceUntil = () => {
    const end = new Date(customUntil)
    if (isNaN(end.getTime()) || end.getTime() <= Date.now()) {
      push('Pick an end time in the future', 'error')
      return
    }
    void act(async () => {
      await enable(monitor.id, new Date().toISOString(), end.toISOString())
      setMaintOpen(false)
      setCustomUntil('')
    }, 'Maintenance started')
  }

  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const inMaintenance = monitor.is_in_maintenance ?? false

  // Action set gated by permission: readonly users get Test only; editable/admin
  // add Pause/Resume + Edit; only owner/admin get Delete (see monitorAccess).
  const actions: ActionItem[] = [
    { key: 'test', label: 'Test', icon: Play, disabled: busy, onClick: () => void act(() => test(), 'Test complete') },
  ]
  if (access.canEdit) {
    actions.push(
      monitor.enabled
        ? { key: 'pause', label: 'Pause', icon: Pause, disabled: busy, onClick: () => void act(() => pause(), 'Monitor paused') }
        : { key: 'resume', label: 'Resume', icon: Play, disabled: busy, onClick: () => void act(() => resume(), 'Monitor resumed') },
      inMaintenance
        ? { key: 'maint', label: 'End maintenance', icon: Wrench, disabled: busy, onClick: () => void act(() => disable(monitor.id), 'Maintenance ended') }
        : { key: 'maint', label: 'Maintenance', icon: Wrench, disabled: busy, onClick: () => setMaintOpen((o) => !o) },
      { key: 'edit', label: 'Edit', icon: Pencil, onClick: () => navigate(`/monitors/${monitor.id}/edit`) }
    )
  }
  if (access.canDelete) {
    actions.push({ key: 'delete', label: 'Delete', icon: Trash2, danger: true, disabled: busy, onClick: () => setConfirmDelete(true) })
  }

  return (
    <div className="space-y-4 border-t border-white/10 p-4">
      {/* Actions row */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-lg font-semibold">{monitor.name}</span>
            <span
              className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                inMaintenance
                  ? 'bg-amber-500/20 text-amber-400'
                  : online
                    ? 'bg-emerald-500/20 text-emerald-400'
                    : offline
                      ? 'bg-red-500/20 text-red-400'
                      : 'bg-white/5 text-slate-400'
              }`}
            >
              {inMaintenance ? 'Maintenance' : online ? 'Online' : offline ? 'Offline' : 'Unknown'}
            </span>
          </div>
          <div className="mt-0.5 text-xs text-slate-400">
            {access.isOwner
              ? 'Your monitor'
              : access.permission === 'admin'
                ? `Owned by ${ownerUsername ?? 'another user'} · admin access`
                : `Shared with you by ${ownerUsername ?? 'another user'} · ${access.permission === 'editable' ? 'can edit' : 'read-only'}`}
          </div>
        </div>
        {/* Desktop: inline buttons. Mobile: dropdown menu. */}
        <div className="hidden flex-wrap gap-1.5 sm:flex">
          {actions.map((a) => (
            <button
              key={a.key}
              disabled={a.disabled}
              onClick={a.onClick}
              className={`btn-secondary !py-1 ${
                a.danger ? '!border-red-500/30 !text-red-400 hover:!bg-red-500/10' : ''
              }`}
            >
              <a.icon className="h-4 w-4" /> {a.label}
            </button>
          ))}
        </div>
        <div className="sm:hidden">
          <ActionMenu items={actions} />
        </div>
      </div>

      {!access.canEdit && (
        <div className="rounded-md border border-white/10 bg-white/5 px-3 py-2 text-xs text-slate-400">
          Read-only access — you can view and test this monitor but not edit it.
        </div>
      )}

      {confirmDelete && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
          <p className="mb-2 text-amber-300">Delete “{monitor.name}” and all its history?</p>
          <div className="flex justify-end gap-2">
            <button className="btn-secondary !py-1" onClick={() => setConfirmDelete(false)}>
              Cancel
            </button>
            <button
              className="btn bg-error-600 !py-1 text-white hover:bg-error-700"
              disabled={deleting}
              onClick={() =>
                void act(async () => {
                  await del()
                  setConfirmDelete(false)
                }, 'Monitor deleted')
              }
            >
              {deleting ? 'Deleting…' : 'Delete'}
            </button>
          </div>
        </div>
      )}

      {/* Maintenance: active banner (with End now) or the quick scheduler. */}
      {inMaintenance && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm"
          style={{ borderColor: 'rgba(255,194,75,0.5)', backgroundColor: 'rgba(255,194,75,0.1)' }}>
          <span className="flex items-center gap-2 font-medium" style={{ color: 'var(--vs-amber)' }}>
            <Wrench className="h-4 w-4" /> Maintenance active
            {monitor.maintenance_end && (
              <span className="font-normal" style={{ color: 'var(--vs-text-dim)' }}>· ends {formatDatetime(monitor.maintenance_end)}</span>
            )}
          </span>
          {access.canEdit && (
            <button className="btn-secondary !py-1" disabled={busy} onClick={() => void act(() => disable(monitor.id), 'Maintenance ended')}>
              End now
            </button>
          )}
        </div>
      )}

      {maintOpen && !inMaintenance && access.canEdit && (
        <div className="rounded-md border p-3 text-sm"
          style={{ borderColor: 'rgba(255,194,75,0.5)', backgroundColor: 'rgba(255,194,75,0.08)' }}>
          <p className="mb-2" style={{ color: 'var(--vs-amber)' }}>
            Start maintenance now — downtime during the window won’t count against uptime or raise incidents.
          </p>
          <div className="flex flex-wrap items-center gap-2">
            {([['1h', 1], ['4h', 4], ['8h', 8], ['24h', 24]] as const).map(([lbl, h]) => (
              <button key={lbl} className="btn-secondary !py-1" disabled={busy} onClick={() => startMaintenance(h)}>
                {lbl}
              </button>
            ))}
            <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>or until</span>
            <input
              type="datetime-local"
              className="rounded-md border border-slate-700 bg-slate-800 px-2 py-1 text-xs text-slate-100"
              value={customUntil}
              onChange={(e) => setCustomUntil(e.target.value)}
            />
            <button className="btn-primary !py-1" disabled={busy || !customUntil} onClick={startMaintenanceUntil}>
              Start
            </button>
            <button className="btn-secondary !py-1" onClick={() => setMaintOpen(false)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Uptime windows */}
      <div className="grid grid-cols-3 gap-2">
        <UptimeBox label="24-hour" pct={uptime?.uptime_24h} />
        <UptimeBox label="7-day" pct={uptime?.uptime_7d} />
        <UptimeBox label="30-day" pct={uptime?.uptime_30d} />
      </div>

      {/* Detailed 24h sparkline */}
      <div>
        <div className="mb-1 flex items-center justify-between text-xs font-medium text-slate-400">
          <span>Uptime (last 24h)</span>
          <span className="flex items-center gap-3">
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.up }} /> up</span>
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.partial }} /> partial</span>
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm" style={{ background: STATUS_COLOR.down }} /> down</span>
          </span>
        </div>
        {uptime ? (
          <Sparkline data={uptime.hourly_data} className="h-10" />
        ) : (
          <div className="flex h-10 items-center text-xs text-slate-400">{uptimeLoading ? 'Loading…' : 'No data'}</div>
        )}
      </div>

      {/* Response time chart */}
      <div>
        <div className="mb-1 text-xs font-medium text-slate-400">Response time (24h)</div>
        <div
          className={
            uptimeLoading || (uptime && uptime.response_time_data.some((pt) => pt.responseTime > 0))
              ? 'h-52'
              : 'py-6'
          }
        >
          {uptimeLoading ? (
            <div className="flex h-full items-center justify-center text-slate-400">
              <Loader2 className="h-5 w-5 animate-spin" />
            </div>
          ) : uptime && uptime.response_time_data.some((p) => p.responseTime > 0) ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={uptime.response_time_data} margin={{ top: 5, right: 10, bottom: 0, left: -8 }}>
                <defs>
                  <linearGradient id={`rt-${monitor.id}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#22d3ee" stopOpacity={0.28} />
                    <stop offset="95%" stopColor="#22d3ee" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#16303a" strokeOpacity={0.6} />
                <XAxis dataKey="time" tick={{ fontSize: 10, fill: '#7A8A94' }} interval={5} minTickGap={16} />
                <YAxis tick={{ fontSize: 10, fill: '#7A8A94' }} width={40} unit="" />
                <Tooltip
                  formatter={(v: number) => [`${v} ms`, 'response']}
                  contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 8, color: '#e2e8f0' }}
                />
                <Area type="monotone" dataKey="responseTime" stroke="#22d3ee" strokeWidth={2} fill={`url(#rt-${monitor.id})`} />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <div className="text-center text-sm text-slate-400">No response data in the last 24h.</div>
          )}
        </div>
      </div>

      {/* Tags + move-to-group */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-slate-400">Tags:</span>
          {monitor.tags && monitor.tags.length > 0 ? (
            monitor.tags.map((t) => (
              <span key={t} className="rounded-full bg-primary-500/20 px-2 py-0.5 text-xs text-primary-300">
                {t}
              </span>
            ))
          ) : (
            <span className="text-xs text-slate-400">none</span>
          )}
        </div>
        {access.canEdit && (
          <label className="flex items-center gap-2 text-xs">
            <span className="text-slate-400">Group:</span>
            <select
              className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-white"
              value={monitor.group_id ?? ''}
              onChange={(e) => void act(() => move(monitor.id, e.target.value || null), 'Monitor group updated')}
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
      </div>

      {/* Incident timeline */}
      <div>
        <div className="mb-1 text-xs font-medium text-slate-400">Recent incidents</div>
        {incidents === null ? (
          <p className="text-xs text-slate-400">Loading…</p>
        ) : incidents.length === 0 ? (
          <p className="text-xs text-slate-400">No incidents in the last 30 days.</p>
        ) : (
          <div className="divide-y divide-white/5">
            {incidents.slice(0, 5).map((inc) => {
              const sev = severityOf(inc)
              const ongoing = inc.end_time === null
              return (
                <div key={inc.id} className="flex items-center justify-between gap-2 py-1.5 text-xs">
                  <span className="flex items-center gap-1.5">
                    <AlertTriangle className="h-3 w-3 text-red-400" />
                    {formatDatetime(inc.start_time)}
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="text-slate-400">
                      {ongoing ? 'ongoing' : `${Math.max(1, Math.round(inc.duration_seconds / 60))} min`}
                    </span>
                    <span className={`rounded px-1.5 py-0.5 font-medium ${sev.cls}`}>{sev.label}</span>
                    <span className={`rounded px-1.5 py-0.5 ${ongoing ? 'bg-red-500/20 text-red-400' : 'bg-white/5 text-slate-400'}`}>
                      {ongoing ? 'ongoing' : 'closed'}
                    </span>
                  </span>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
