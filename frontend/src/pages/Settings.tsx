import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ExternalLink, Loader2 } from 'lucide-react'
import api from '@/services/api'
import { useToasts, Toaster } from '@/components/Toast'
import SettingsCard from '@/components/SettingsCard'
import BackupRestore from '@/components/BackupRestore'
import ReconnectAgent from '@/components/ReconnectAgent'
import TimezoneSelector from '@/components/TimezoneSelector'
import NotificationSettings from '@/pages/NotificationSettings'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig, DEFAULT_APP_NAME } from '@/context/AppConfigContext'
import { useIncidentRetention } from '@/hooks/useIncidents'
import { useSystemVersion } from '@/hooks/useSystemVersion'

type Tab = 'system' | 'notifications' | 'about'

const GITHUB_URL = 'https://github.com/Stevy2191/Sentinel'

/** True for a version CI actually tagged, e.g. "0.1.0" — false for "dev" or a branch name. */
const isReleasedVersion = (v: string) => /^\d+\.\d+\.\d+/.test(v)

// Mirrors the bounds the backend enforces (models.Min/MaxCheckIntervalSeconds),
// which are themselves the monitor validator's bounds — so a value accepted
// here is always a value a monitor may actually hold.
const MIN_INTERVAL = 10
const MAX_INTERVAL = 3600
const MIN_CHECK_RETENTION = 1
const MAX_CHECK_RETENTION = 3650
const MAX_APP_NAME = 40
const MIN_SLA_TARGET = 0
const MAX_SLA_TARGET = 100

const TAB_LABEL: Record<Tab, string> = {
  system: 'System',
  notifications: 'Notifications',
  about: 'About',
}

/** One address's reachability result from the server-side probe. */
interface UrlProbe {
  url: string
  reachable: boolean
  status?: number
  error?: string
  source: string
}

interface SystemSettings {
  app_name: string
  base_url: string
  default_check_interval: number
  /** How long individual check results are kept. */
  check_retention_days: number
  /** Where agent install commands download from. Empty means derive it. */
  sentinel_external_url: string
  /** Where agents report metrics. Empty means use the external URL. */
  sentinel_internal_url: string
  /** IANA zone that reports are rendered in and the UI displays times in. */
  report_timezone: string
  /** Uptime percentage a monitor is held to when it has no override of its own. */
  default_sla_target: number
}

/**
 * Reads the API's error message, falling back to something actionable.
 *
 * The API has two error shapes: respondError sends `error` as a plain string,
 * respondAuthError sends it as `{code, message}`. Handling only one of them
 * silently swallows half the validation messages and shows the fallback
 * instead, so both are read here.
 */
function apiMessage(err: unknown, fallback: string): string {
  const body = (err as { response?: { data?: { error?: unknown } } })?.response?.data?.error
  if (typeof body === 'string' && body.trim()) return body
  if (body && typeof body === 'object') {
    const msg = (body as { message?: unknown }).message
    if (typeof msg === 'string' && msg.trim()) return msg
  }
  return fallback
}

export default function Settings() {
  const { toasts, push } = useToasts()
  const { currentUser } = useAuthContext()
  const { appName, refresh: refreshAppConfig } = useAppConfig()
  const isAdmin = currentUser?.is_admin ?? false
  const version = useSystemVersion()

  // System and notification-channel settings are instance-wide and their APIs
  // are gated by RequireAdmin, so a non-admin is not shown tabs they cannot use.
  const tabs = useMemo<Tab[]>(
    () =>
      (['system', 'notifications', 'about'] as Tab[]).filter(
        (t) => (t !== 'notifications' && t !== 'system') || isAdmin
      ),
    [isAdmin]
  )
  const [tab, setTab] = useState<Tab>(() => (isAdmin ? 'system' : 'about'))

  // ---- Incident retention (server-side, admin-only) ----
  const { days: retentionDays, bounds: retentionBounds, save: saveRetention } = useIncidentRetention()
  const [retention, setRetention] = useState<number | null>(null)
  const [retentionSaving, setRetentionSaving] = useState(false)
  // Seeded from the server once it answers, then owned by the field so typing
  // is not overwritten by a later render.
  useEffect(() => {
    if (retentionDays != null) setRetention((r) => r ?? retentionDays)
  }, [retentionDays])

  const retentionValid =
    retention != null && retention >= retentionBounds.min && retention <= retentionBounds.max

  const persistRetention = async () => {
    if (retention == null || !retentionValid) return
    setRetentionSaving(true)
    try {
      await saveRetention(retention)
      push(`Incident history kept for ${retention} days`, 'success')
    } catch (err) {
      push(apiMessage(err, 'Could not update retention'), 'error')
    } finally {
      setRetentionSaving(false)
    }
  }

  // ---- System (server-side, admin-only) ----
  const [system, setSystem] = useState<SystemSettings | null>(null)
  const [systemLoading, setSystemLoading] = useState(isAdmin)
  const [systemSaving, setSystemSaving] = useState(false)
  const [systemError, setSystemError] = useState<string | null>(null)

  useEffect(() => {
    if (!isAdmin) return
    let active = true
    setSystemLoading(true)
    api
      .get<{ data: SystemSettings }>('/settings')
      .then((r) => {
        if (!active) return
        setSystem({
          app_name: r.data.data.app_name || DEFAULT_APP_NAME,
          base_url: r.data.data.base_url ?? '',
          default_check_interval: r.data.data.default_check_interval,
          check_retention_days: r.data.data.check_retention_days,
          sentinel_external_url: r.data.data.sentinel_external_url ?? '',
          sentinel_internal_url: r.data.data.sentinel_internal_url ?? '',
          report_timezone: r.data.data.report_timezone || 'UTC',
          default_sla_target: r.data.data.default_sla_target,
        })
        setSystemError(null)
      })
      .catch((err) => active && setSystemError(apiMessage(err, 'Could not load system settings')))
      .finally(() => active && setSystemLoading(false))
    return () => {
      active = false
    }
  }, [isAdmin])

  const saveSystem = async () => {
    if (!system) return
    setSystemSaving(true)
    try {
      await api.patch('/settings/system', {
        app_name: system.app_name.trim(),
        base_url: system.base_url.trim(),
        default_check_interval: system.default_check_interval,
        check_retention_days: system.check_retention_days,
        sentinel_external_url: system.sentinel_external_url.trim(),
        sentinel_internal_url: system.sentinel_internal_url.trim(),
        report_timezone: system.report_timezone,
        default_sla_target: system.default_sla_target,
      })
      // Re-read the public config so the sidebar, sign-in screen and browser tab
      // pick up a renamed instance without a reload.
      await refreshAppConfig()
      setSystemError(null)
      push('System settings saved', 'success')
    } catch (err) {
      const msg = apiMessage(err, 'Could not save system settings')
      setSystemError(msg)
      push(msg, 'error')
    } finally {
      setSystemSaving(false)
    }
  }

  const intervalValid =
    Number.isFinite(system?.default_check_interval) &&
    (system?.default_check_interval ?? 0) >= MIN_INTERVAL &&
    (system?.default_check_interval ?? 0) <= MAX_INTERVAL
  const slaValid =
    Number.isFinite(system?.default_sla_target) &&
    (system?.default_sla_target ?? -1) >= MIN_SLA_TARGET &&
    (system?.default_sla_target ?? -1) <= MAX_SLA_TARGET
  const checkRetentionValid =
    !system ||
    (Number.isFinite(system.check_retention_days) &&
      system.check_retention_days >= MIN_CHECK_RETENTION &&
      system.check_retention_days <= MAX_CHECK_RETENTION)
  // Validated the same way the server does, so the button does not offer to
  // save something that will be refused.
  const urlOK = (v: string) => {
    const raw = v.trim().replace(/\/+$/, '')
    if (raw === '') return true
    try {
      const u = new URL(raw)
      return (u.protocol === 'http:' || u.protocol === 'https:') && !!u.host && (u.pathname === '' || u.pathname === '/')
    } catch {
      return false
    }
  }
  const urlsValid =
    !system || (urlOK(system.sentinel_external_url) && urlOK(system.sentinel_internal_url))

  const [urlTesting, setUrlTesting] = useState(false)
  const [urlTest, setUrlTest] = useState<{
    external: UrlProbe
    internal: UrlProbe
  } | null>(null)

  const testURLs = async () => {
    if (!system) return
    setUrlTesting(true)
    try {
      const res = await api.post<{ data: { external: UrlProbe; internal: UrlProbe } }>(
        '/agents/urls/test',
        {
          // The values on screen, not the saved ones, so a URL can be checked
          // before it is committed.
          external_url: system.sentinel_external_url.trim(),
          internal_url: system.sentinel_internal_url.trim(),
        },
      )
      setUrlTest(res.data.data)
    } catch (err) {
      push(apiMessage(err, 'Could not test the URLs'), 'error')
    } finally {
      setUrlTesting(false)
    }
  }

  const nameValid = !!system && system.app_name.trim().length > 0 && system.app_name.trim().length <= MAX_APP_NAME

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Settings</h1>
        <p className="text-sm text-slate-400">
          {isAdmin ? `Configure this ${appName} instance` : 'Instance information'}
        </p>
      </div>

      {/* Tabs */}
      <div className="flex flex-wrap gap-2">
        {tabs.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`rounded-md px-4 py-2 text-sm font-medium ${
              tab === t ? 'bg-primary-600 text-white' : 'bg-white/5 text-slate-300'
            }`}
          >
            {TAB_LABEL[t]}
          </button>
        ))}
      </div>

      {tab === 'system' && isAdmin && (
        <div className="space-y-6">
          <p className="text-sm text-slate-400">
            These apply to the whole instance and to everyone who uses it, not just to you.
          </p>

          {systemError && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
              {systemError}
            </div>
          )}

          {systemLoading || !system ? (
            <div className="flex items-center gap-2 p-6 text-sm text-slate-400">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading system settings…
            </div>
          ) : (
            <>
              <SettingsCard
                title="Application Name"
                description="Shown in the sidebar, on the sign-in screen, in the browser tab and in outgoing email."
              >
                <input
                  value={system.app_name}
                  maxLength={MAX_APP_NAME}
                  onChange={(e) => setSystem({ ...system, app_name: e.target.value })}
                  placeholder={DEFAULT_APP_NAME}
                  aria-label="Application name"
                  className="w-full"
                />
                {!nameValid && (
                  <p className="text-xs text-red-400">
                    A name is required, up to {MAX_APP_NAME} characters.
                  </p>
                )}
              </SettingsCard>

              <SettingsCard
                title="Public URL"
                description="The address people reach this instance at. Every link in outgoing email is built from it, so it must be what a recipient can open — not what the server sees."
              >
                <input
                  value={system.base_url}
                  onChange={(e) => setSystem({ ...system, base_url: e.target.value })}
                  placeholder="https://sentinel.example.com"
                  inputMode="url"
                  aria-label="Public URL"
                  className="w-full"
                />
                <p className="text-xs text-slate-500">
                  Leave empty to omit links from email rather than send broken ones.
                </p>
              </SettingsCard>

              <SettingsCard
                title="Default Check Interval"
                description="How often a newly created monitor checks, in seconds. Existing monitors keep their own interval."
              >
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={MIN_INTERVAL}
                    max={MAX_INTERVAL}
                    value={system.default_check_interval}
                    onChange={(e) =>
                      setSystem({ ...system, default_check_interval: Number(e.target.value) })
                    }
                    aria-label="Default check interval in seconds"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">seconds</span>
                </div>
                {!intervalValid && (
                  <p className="text-xs text-red-400">
                    Must be between {MIN_INTERVAL} and {MAX_INTERVAL} seconds.
                  </p>
                )}
              </SettingsCard>

              <SettingsCard
                title="Default SLA Target"
                description="The uptime percentage a monitor is held to when it has no target of its own. Reports compare against this unless a monitor overrides it."
              >
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={MIN_SLA_TARGET}
                    max={MAX_SLA_TARGET}
                    step={0.1}
                    value={system.default_sla_target}
                    onChange={(e) =>
                      setSystem({ ...system, default_sla_target: Number(e.target.value) })
                    }
                    aria-label="Default SLA target percentage"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">%</span>
                </div>
                {!slaValid && (
                  <p className="text-xs text-red-400">
                    Must be between {MIN_SLA_TARGET} and {MAX_SLA_TARGET}.
                  </p>
                )}
              </SettingsCard>

              <SettingsCard
                title="Sentinel URLs"
                description="How monitored servers reach Sentinel. Leave both empty unless a reverse proxy makes the address a browser uses different from the one an agent can reach."
              >
                <label htmlFor="external-url" className="block text-sm font-medium text-white">
                  External URL — where install scripts download from
                </label>
                <input
                  id="external-url"
                  type="text"
                  value={system.sentinel_external_url}
                  onChange={(e) => setSystem({ ...system, sentinel_external_url: e.target.value })}
                  placeholder={`auto: ${window.location.origin}`}
                  className="w-full"
                />
                <label htmlFor="internal-url" className="mt-3 block text-sm font-medium text-white">
                  Internal URL — where agents report metrics
                </label>
                <input
                  id="internal-url"
                  type="text"
                  value={system.sentinel_internal_url}
                  onChange={(e) => setSystem({ ...system, sentinel_internal_url: e.target.value })}
                  placeholder="same as the external URL"
                  className="w-full"
                />
                <p className={`text-xs ${urlsValid ? 'text-slate-500' : 'text-red-400'}`}>
                  {urlsValid
                    ? 'Empty means Sentinel works the address out from your browser, which is right when there is no proxy. Include the scheme and port but no path, e.g. http://10.1.20.10:3001.'
                    : 'Each URL must include a scheme and host and no path, e.g. https://sentinel.example.com or http://10.1.20.10:3001.'}
                </p>

                <div className="flex flex-wrap items-center gap-2 pt-1">
                  <button
                    className="btn-secondary !py-1"
                    disabled={urlTesting || !urlsValid}
                    onClick={() => void testURLs()}
                  >
                    {urlTesting ? 'Testing…' : 'Test both URLs'}
                  </button>
                  <span className="text-xs text-slate-500">
                    Checked from the Sentinel server, which is what an agent has to reach
                  </span>
                </div>

                {urlTest && (
                  <div className="space-y-1">
                    {(['external', 'internal'] as const).map((k) => {
                      const probe = urlTest[k]
                      return (
                        <p
                          key={k}
                          className={`text-xs ${probe.reachable ? 'text-emerald-400' : 'text-amber-300'}`}
                        >
                          {probe.reachable ? '✓' : '✕'} {k}: {probe.url || '(none)'} —{' '}
                          {probe.reachable ? `responded ${probe.status}` : probe.error}
                          <span className="text-slate-600"> ({probe.source})</span>
                        </p>
                      )
                    })}
                  </div>
                )}
              </SettingsCard>

              {/* Instance-wide rather than per-user: a rendered report is a file
                  that gets emailed, downloaded and shared, so the times inside it
                  must mean the same thing to everyone who opens it rather than
                  depending on who pressed Generate. */}
              <SettingsCard
                title="Timezone"
                description="The zone reports are written in, and that times are displayed in across Sentinel."
              >
                <TimezoneSelector
                  value={system.report_timezone}
                  onChange={(tz) => setSystem({ ...system, report_timezone: tz })}
                />
                <p className="text-xs text-slate-500">
                  Applies to generated PDFs, scheduled report emails and every timestamp on
                  screen. Takes effect on the next report — nothing needs restarting.
                </p>
              </SettingsCard>
              
              <SettingsCard
                title="Data Retention"
                description="Old history is deleted automatically. The purge runs nightly at 2 AM."
              >
                {/* Two windows, because a check is a measurement and an
                    incident is a conclusion drawn from many of them. Checks
                    arrive one per monitor per interval, so they are far more
                    numerous and rarely worth keeping as long. */}
                <label htmlFor="check-retention-days" className="block text-sm font-medium text-white">
                  Keep check history for
                </label>
                <div className="flex items-center gap-2">
                  <input
                    id="check-retention-days"
                    type="number"
                    min={MIN_CHECK_RETENTION}
                    max={MAX_CHECK_RETENTION}
                    value={system.check_retention_days}
                    onChange={(e) =>
                      setSystem({ ...system, check_retention_days: Number(e.target.value) })
                    }
                    aria-label="Check retention in days"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">days</span>
                </div>
                <p className={`mb-4 mt-1 text-xs ${checkRetentionValid ? 'text-slate-500' : 'text-red-400'}`}>
                  {checkRetentionValid
                    ? 'Every check is recorded, so this is the largest table by far — one monitor checked each minute adds over half a million rows a year. Response-time graphs and reports only reach back this far. Saved with the other system settings.'
                    : `Must be between ${MIN_CHECK_RETENTION} and ${MAX_CHECK_RETENTION} days.`}
                </p>

                <label htmlFor="retention-days" className="block text-sm font-medium text-white">
                  Keep incident history for
                </label>
                <div className="flex items-center gap-2">
                  <input
                    id="retention-days"
                    type="number"
                    min={retentionBounds.min}
                    max={retentionBounds.max}
                    value={retention ?? ''}
                    onChange={(e) => setRetention(Number(e.target.value))}
                    aria-label="Incident retention in days"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">days</span>
                  <button
                    className="btn-secondary !py-1"
                    disabled={!retentionValid || retentionSaving || retention === retentionDays}
                    onClick={() => void persistRetention()}
                  >
                    {retentionSaving ? 'Saving…' : 'Save'}
                  </button>
                </div>
                {!retentionValid && retention != null && (
                  <p className="text-xs text-red-400">
                    Must be between {retentionBounds.min} and {retentionBounds.max} days.
                  </p>
                )}
                {/* An ongoing incident is never purged however old it is: it is
                    still happening, and is the one most likely to be looked at. */}
                <p className="text-xs text-slate-500">
                  Only resolved incidents are removed. An ongoing incident is kept until it ends.
                </p>
              </SettingsCard>

              <ReconnectAgent push={push} />

              <BackupRestore push={push} />

              <div className="flex items-center justify-between border-t border-white/10 pt-4">
                <p className="text-xs text-slate-500">
                  Who may create an account is on the{' '}
                  <Link className="text-primary-400 hover:underline" to="/admin/users">
                    Users
                  </Link>{' '}
                  page.
                </p>
                <button
                  className="btn-primary"
                  disabled={systemSaving || !nameValid || !intervalValid || !checkRetentionValid || !urlsValid || !slaValid}
                  onClick={() => void saveSystem()}
                >
                  {systemSaving ? 'Saving…' : 'Save System Settings'}
                </button>
              </div>
            </>
          )}
        </div>
      )}

      {tab === 'notifications' && isAdmin && <NotificationSettings />}

      {tab === 'about' && (
        <div className="space-y-6">
          <SettingsCard title="Application">
            <dl className="space-y-2 text-sm">
              <div className="flex justify-between">
                <dt className="text-slate-500">Name</dt>
                <dd className="font-medium">{appName}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-500">Version</dt>
                <dd className="font-medium">
                  {version === null ? (
                    '…'
                  ) : isReleasedVersion(version) ? (
                    <a
                      className="hover:underline"
                      href={`${GITHUB_URL}/releases/tag/v${version}`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      Sentinel v{version}
                    </a>
                  ) : (
                    `Sentinel (development build)`
                  )}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-500">License</dt>
                <dd className="font-medium">
                  <a
                    className="hover:underline"
                    href={`${GITHUB_URL}/blob/main/LICENSE`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    AGPL-3.0
                  </a>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-500">Frontend</dt>
                <dd className="font-medium">React + TypeScript + Vite</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-500">Database</dt>
                <dd className="font-medium">PostgreSQL</dd>
              </div>
            </dl>
          </SettingsCard>
          <SettingsCard title="Links">
            <div className="flex flex-wrap gap-2">
              <a className="btn-secondary" href={`${GITHUB_URL}#readme`} target="_blank" rel="noreferrer">
                <ExternalLink className="h-4 w-4" /> Documentation
              </a>
              <a className="btn-secondary" href={GITHUB_URL} target="_blank" rel="noreferrer">
                <ExternalLink className="h-4 w-4" /> GitHub
              </a>
              <a className="btn-secondary" href={`${GITHUB_URL}/issues`} target="_blank" rel="noreferrer">
                <ExternalLink className="h-4 w-4" /> Report Issue
              </a>
            </div>
          </SettingsCard>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
