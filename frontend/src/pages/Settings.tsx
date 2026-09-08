import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { format } from 'date-fns'
import { Volume2, ExternalLink, Loader2 } from 'lucide-react'
import api from '@/services/api'
import { useToasts, Toaster } from '@/components/Toast'
import SettingsCard from '@/components/SettingsCard'
import TimezoneSelector from '@/components/TimezoneSelector'
import NotificationSettings from '@/pages/NotificationSettings'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig, DEFAULT_APP_NAME } from '@/context/AppConfigContext'
import { useIncidentRetention } from '@/hooks/useIncidents'
import {
  PREF,
  DEFAULTS,
  applyStoredPreferences,
  resetAllPreferences,
  defaultTimezone,
  getString,
  getBool,
  setString,
  setBool,
  type FontSize,
  type TimeFormat,
  type DateFormatPref,
  type ReportRange,
} from '@/utils/preferences'

type Tab = 'system' | 'preferences' | 'notifications' | 'about'

const GITHUB_URL = 'https://github.com/Stevy2191/Sentinel'

// Mirrors the bounds the backend enforces (models.Min/MaxCheckIntervalSeconds),
// which are themselves the monitor validator's bounds — so a value accepted
// here is always a value a monitor may actually hold.
const MIN_INTERVAL = 10
const MAX_INTERVAL = 3600
const MAX_APP_NAME = 40

const TAB_LABEL: Record<Tab, string> = {
  system: 'System',
  preferences: 'Preferences',
  notifications: 'Notifications',
  about: 'About',
}

const dateFmtMap: Record<DateFormatPref, string> = {
  'MMM DD, YYYY': 'MMM dd, yyyy',
  'DD/MM/YYYY': 'dd/MM/yyyy',
  'YYYY-MM-DD': 'yyyy-MM-dd',
}

interface SystemSettings {
  app_name: string
  base_url: string
  default_check_interval: number
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-4">
      <span className="text-sm">{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
          checked ? 'bg-primary-600' : 'bg-white/10'
        }`}
      >
        <span
          className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${
            checked ? 'translate-x-5' : 'translate-x-0'
          }`}
        />
      </button>
    </label>
  )
}

function RadioRow<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { value: T; label: string }[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={`rounded-md px-4 py-2 text-sm font-medium ${
            value === o.value ? 'bg-primary-600 text-white' : 'bg-white/5 text-slate-300'
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
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

  // System and notification-channel settings are instance-wide and their APIs
  // are gated by RequireAdmin, so a non-admin is not shown tabs they cannot use.
  const tabs = useMemo<Tab[]>(
    () =>
      (['system', 'preferences', 'notifications', 'about'] as Tab[]).filter(
        (t) => (t !== 'notifications' && t !== 'system') || isAdmin
      ),
    [isAdmin]
  )
  const [tab, setTab] = useState<Tab>(() => (isAdmin ? 'system' : 'preferences'))

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

  // ---- Per-user preferences (this browser) ----
  const [fontSize, setFontSize] = useState<FontSize>(
    () => getString(PREF.fontSize, DEFAULTS.fontSize) as FontSize
  )
  const [soundAlerts, setSoundAlerts] = useState(() => getBool(PREF.soundAlerts, DEFAULTS.soundAlerts))
  const [desktopNotifications, setDesktopNotifications] = useState(() =>
    getBool(PREF.desktopNotifications, DEFAULTS.desktopNotifications)
  )
  const [timeFormat, setTimeFormat] = useState<TimeFormat>(
    () => getString(PREF.timeFormat, DEFAULTS.timeFormat) as TimeFormat
  )
  const [timezone, setTimezone] = useState(() => getString(PREF.timezone, defaultTimezone()))
  const [dateFormat, setDateFormat] = useState<DateFormatPref>(
    () => getString(PREF.dateFormat, DEFAULTS.dateFormat) as DateFormatPref
  )
  const [reportRange, setReportRange] = useState<ReportRange>(
    () => getString(PREF.reportRange, DEFAULTS.reportRange) as ReportRange
  )

  const [confirmReset, setConfirmReset] = useState(false)

  const changeFontSize = (v: FontSize) => {
    setFontSize(v)
    setString(PREF.fontSize, v)
    applyStoredPreferences()
  }
  const toggleSound = (v: boolean) => {
    setSoundAlerts(v)
    setBool(PREF.soundAlerts, v)
    if (v) playBeep()
  }
  const toggleDesktop = async (v: boolean) => {
    if (v && 'Notification' in window) {
      const perm = await Notification.requestPermission()
      if (perm !== 'granted') {
        push('Desktop notification permission denied', 'error')
        setDesktopNotifications(false)
        setBool(PREF.desktopNotifications, false)
        return
      }
    }
    setDesktopNotifications(v)
    setBool(PREF.desktopNotifications, v)
  }

  const savePreferences = () => {
    setString(PREF.fontSize, fontSize)
    setBool(PREF.soundAlerts, soundAlerts)
    setBool(PREF.desktopNotifications, desktopNotifications)
    setString(PREF.timeFormat, timeFormat)
    setString(PREF.timezone, timezone)
    setString(PREF.dateFormat, dateFormat)
    setString(PREF.reportRange, reportRange)
    applyStoredPreferences()
    push('Preferences saved', 'success')
  }

  const doReset = () => {
    resetAllPreferences()
    setFontSize(DEFAULTS.fontSize)
    setSoundAlerts(DEFAULTS.soundAlerts)
    setDesktopNotifications(DEFAULTS.desktopNotifications)
    setTimeFormat(DEFAULTS.timeFormat)
    setTimezone(defaultTimezone())
    setDateFormat(DEFAULTS.dateFormat)
    setReportRange(DEFAULTS.reportRange)
    applyStoredPreferences()
    setConfirmReset(false)
    push('Preferences reset to defaults', 'success')
  }

  const now = new Date()
  const intervalValid =
    Number.isFinite(system?.default_check_interval) &&
    (system?.default_check_interval ?? 0) >= MIN_INTERVAL &&
    (system?.default_check_interval ?? 0) <= MAX_INTERVAL
  const nameValid = !!system && system.app_name.trim().length > 0 && system.app_name.trim().length <= MAX_APP_NAME

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Settings</h1>
        <p className="text-sm text-slate-400">
          {isAdmin
            ? `Configure this ${appName} instance and your own preferences`
            : 'Your preferences on this device'}
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
                title="Data Retention"
                description="Incidents older than this are deleted automatically. The purge runs nightly at 2 AM."
              >
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
                  disabled={systemSaving || !nameValid || !intervalValid}
                  onClick={() => void saveSystem()}
                >
                  {systemSaving ? 'Saving…' : 'Save System Settings'}
                </button>
              </div>
            </>
          )}
        </div>
      )}

      {tab === 'preferences' && (
        <div className="space-y-6">
          <p className="text-sm text-slate-400">These are stored in this browser and affect only you.</p>

          <SettingsCard title="Font Size" description="Scales text across the app.">
            <RadioRow<FontSize>
              value={fontSize}
              onChange={changeFontSize}
              options={[
                { value: 'compact', label: 'Compact (90%)' },
                { value: 'normal', label: 'Normal (100%)' },
                { value: 'large', label: 'Large (110%)' },
              ]}
            />
          </SettingsCard>

          <SettingsCard title="Notifications">
            <Toggle label="Play sound when alerts occur" checked={soundAlerts} onChange={toggleSound} />
            <button className="btn-secondary !py-1" onClick={playBeep}>
              <Volume2 className="h-4 w-4" /> Test sound
            </button>
            <Toggle
              label="Show browser notifications for critical alerts"
              checked={desktopNotifications}
              onChange={(v) => void toggleDesktop(v)}
            />
          </SettingsCard>

          <SettingsCard title="Time Format">
            <RadioRow<TimeFormat>
              value={timeFormat}
              onChange={(v) => {
                setTimeFormat(v)
                setString(PREF.timeFormat, v)
              }}
              options={[
                { value: '12h', label: '12-hour' },
                { value: '24h', label: '24-hour' },
              ]}
            />
            <div className="text-sm text-slate-500">
              Preview: {format(now, timeFormat === '12h' ? 'h:mm:ss a' : 'HH:mm:ss')}
            </div>
          </SettingsCard>

          <SettingsCard title="Timezone" description="Used for displaying report timestamps.">
            <TimezoneSelector
              value={timezone}
              onChange={(tz) => {
                setTimezone(tz)
                setString(PREF.timezone, tz)
              }}
            />
          </SettingsCard>

          <SettingsCard title="Date Format">
            <RadioRow<DateFormatPref>
              value={dateFormat}
              onChange={(v) => {
                setDateFormat(v)
                setString(PREF.dateFormat, v)
              }}
              options={[
                { value: 'MMM DD, YYYY', label: 'MMM DD, YYYY' },
                { value: 'DD/MM/YYYY', label: 'DD/MM/YYYY' },
                { value: 'YYYY-MM-DD', label: 'YYYY-MM-DD' },
              ]}
            />
            <div className="text-sm text-slate-500">Preview: {format(now, dateFmtMap[dateFormat])}</div>
          </SettingsCard>

          <SettingsCard title="Default Report Range">
            <RadioRow<ReportRange>
              value={reportRange}
              onChange={(v) => {
                setReportRange(v)
                setString(PREF.reportRange, v)
              }}
              options={[
                { value: '7d', label: 'Last 7 days' },
                { value: '30d', label: 'Last 30 days' },
                { value: '90d', label: 'Last 90 days' },
                { value: 'custom', label: 'Custom' },
              ]}
            />
          </SettingsCard>

          {/* Scoped to this tab: these buttons only touch browser-local
              preferences. System settings save themselves on their own tab. */}
          <div className="flex items-center justify-between border-t border-white/10 pt-4">
            <button className="btn-secondary text-red-400" onClick={() => setConfirmReset(true)}>
              Reset to Defaults
            </button>
            <button className="btn-primary" onClick={savePreferences}>
              Save Preferences
            </button>
          </div>
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
                <dd className="font-medium">Sentinel v1.0</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-slate-500">License</dt>
                <dd className="font-medium">MIT</dd>
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

      {confirmReset && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm p-6">
            <h3 className="text-lg font-semibold">Reset preferences?</h3>
            <p className="mt-2 text-sm text-slate-400">
              This restores your browser preferences — font size, sound, time and date format — to
              their defaults. System settings are not affected.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmReset(false)}>
                Cancel
              </button>
              <button className="btn-danger" onClick={doReset}>
                Reset
              </button>
            </div>
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

function playBeep() {
  try {
    const ctx = new AudioContext()
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.connect(gain)
    gain.connect(ctx.destination)
    osc.frequency.value = 880
    gain.gain.value = 0.1
    osc.start()
    window.setTimeout(() => {
      osc.stop()
      void ctx.close()
    }, 200)
  } catch {
    /* audio not available */
  }
}
