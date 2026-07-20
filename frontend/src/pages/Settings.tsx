import { useMemo, useState } from 'react'
import { format } from 'date-fns'
import { Upload, Trash2, Volume2, ExternalLink } from 'lucide-react'
import { useTheme, type ThemeMode } from '@/context/ThemeContext'
import { useToasts, Toaster } from '@/components/Toast'
import SettingsCard from '@/components/SettingsCard'
import ColorPicker from '@/components/ColorPicker'
import TimezoneSelector from '@/components/TimezoneSelector'
import NotificationSettings from '@/pages/NotificationSettings'
import { useAuthContext } from '@/context/AuthContext'
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
  type CardLayout,
  type TimeFormat,
  type DateFormatPref,
  type ReportRange,
} from '@/utils/preferences'

type Tab = 'appearance' | 'preferences' | 'notifications' | 'about'

const GITHUB_URL = 'https://github.com/Stevy2191/Sentinel'

const dateFmtMap: Record<DateFormatPref, string> = {
  'MMM DD, YYYY': 'MMM dd, yyyy',
  'DD/MM/YYYY': 'dd/MM/yyyy',
  'YYYY-MM-DD': 'yyyy-MM-dd',
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
          checked ? 'bg-primary-600' : 'bg-neutral-300 dark:bg-neutral-700'
        }`}
      >
        <span
          className={`absolute top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${
            checked ? 'translate-x-[22px]' : 'translate-x-0.5'
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
            value === o.value
              ? 'bg-primary-600 text-white'
              : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export default function Settings() {
  const { mode, setMode } = useTheme()
  const { toasts, push } = useToasts()
  const { currentUser } = useAuthContext()
  const isAdmin = currentUser?.is_admin ?? false
  // Notification-channel config is admin-only (the API is gated by RequireAdmin),
  // so only show the tab to admins.
  const tabs = useMemo<Tab[]>(
    () =>
      (['appearance', 'preferences', 'notifications', 'about'] as Tab[]).filter(
        (t) => t !== 'notifications' || isAdmin
      ),
    [isAdmin]
  )
  const [tab, setTab] = useState<Tab>('appearance')

  // Appearance
  const [logo, setLogo] = useState(() => getString(PREF.logo, ''))
  const [primary, setPrimary] = useState(() => getString(PREF.primaryColor, DEFAULTS.primaryColor))
  const [accent, setAccent] = useState(() => getString(PREF.accentColor, DEFAULTS.accentColor))
  const [fontSize, setFontSize] = useState<FontSize>(
    () => getString(PREF.fontSize, DEFAULTS.fontSize) as FontSize
  )
  const [sidebarExpanded, setSidebarExpanded] = useState(() =>
    getBool(PREF.sidebarExpanded, DEFAULTS.sidebarExpanded)
  )
  const [cardLayout, setCardLayout] = useState<CardLayout>(
    () => getString(PREF.cardLayout, DEFAULTS.cardLayout) as CardLayout
  )

  // Preferences
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

  // --- Appearance handlers ---
  const changePrimary = (hex: string) => {
    setPrimary(hex)
    setString(PREF.primaryColor, hex)
    applyStoredPreferences()
  }
  const changeAccent = (hex: string) => {
    setAccent(hex)
    setString(PREF.accentColor, hex)
    applyStoredPreferences()
  }
  const changeFontSize = (v: FontSize) => {
    setFontSize(v)
    setString(PREF.fontSize, v)
    applyStoredPreferences()
  }
  const onLogoFile = (file: File | undefined) => {
    if (!file) return
    if (!['image/png', 'image/jpeg'].includes(file.type)) {
      push('Logo must be a PNG or JPG', 'error')
      return
    }
    if (file.size > 2 * 1024 * 1024) {
      push('Logo must be under 2MB', 'error')
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const data = reader.result as string
      setLogo(data)
      setString(PREF.logo, data)
      push('Logo updated', 'success')
    }
    reader.readAsDataURL(file)
  }
  const deleteLogo = () => {
    setLogo('')
    setString(PREF.logo, '')
    push('Logo removed', 'info')
  }

  // --- Preferences handlers ---
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

  const saveAll = () => {
    setString(PREF.primaryColor, primary)
    setString(PREF.accentColor, accent)
    setString(PREF.fontSize, fontSize)
    setBool(PREF.sidebarExpanded, sidebarExpanded)
    setString(PREF.cardLayout, cardLayout)
    setBool(PREF.soundAlerts, soundAlerts)
    setBool(PREF.desktopNotifications, desktopNotifications)
    setString(PREF.timeFormat, timeFormat)
    setString(PREF.timezone, timezone)
    setString(PREF.dateFormat, dateFormat)
    setString(PREF.reportRange, reportRange)
    applyStoredPreferences()
    push('Settings saved successfully', 'success')
  }

  const doReset = () => {
    resetAllPreferences()
    setMode('auto')
    setLogo('')
    setPrimary(DEFAULTS.primaryColor)
    setAccent(DEFAULTS.accentColor)
    setFontSize(DEFAULTS.fontSize)
    setSidebarExpanded(DEFAULTS.sidebarExpanded)
    setCardLayout(DEFAULTS.cardLayout)
    setSoundAlerts(DEFAULTS.soundAlerts)
    setDesktopNotifications(DEFAULTS.desktopNotifications)
    setTimeFormat(DEFAULTS.timeFormat)
    setTimezone(defaultTimezone())
    setDateFormat(DEFAULTS.dateFormat)
    setReportRange(DEFAULTS.reportRange)
    applyStoredPreferences()
    setConfirmReset(false)
    push('All settings reset to defaults', 'success')
  }

  const now = new Date()

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Customize Sentinel to your preferences
        </p>
      </div>

      {/* Tabs */}
      <div className="flex flex-wrap gap-2">
        {tabs.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`rounded-md px-4 py-2 text-sm font-medium capitalize ${
              tab === t
                ? 'bg-primary-600 text-white'
                : 'bg-white text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'appearance' && (
        <div className="space-y-6">
          <SettingsCard title="Logo" description="Shown in the app header. PNG or JPG, max 2MB.">
            <div className="flex items-center gap-4">
              {logo ? (
                <img src={logo} alt="Logo" className="h-14 w-14 rounded-md object-contain" />
              ) : (
                <div className="flex h-14 w-14 items-center justify-center rounded-md border border-dashed border-neutral-300 text-xs text-neutral-400 dark:border-neutral-700">
                  None
                </div>
              )}
              <label className="btn-secondary cursor-pointer">
                <Upload className="h-4 w-4" /> Upload
                <input
                  type="file"
                  accept="image/png,image/jpeg"
                  className="hidden"
                  onChange={(e) => onLogoFile(e.target.files?.[0])}
                />
              </label>
              {logo && (
                <button className="btn-secondary text-error-600" onClick={deleteLogo}>
                  <Trash2 className="h-4 w-4" /> Remove
                </button>
              )}
            </div>
          </SettingsCard>

          <SettingsCard title="Theme">
            <RadioRow<ThemeMode>
              value={mode}
              onChange={setMode}
              options={[
                { value: 'auto', label: 'Auto' },
                { value: 'light', label: 'Light' },
                { value: 'dark', label: 'Dark' },
              ]}
            />
          </SettingsCard>

          <SettingsCard title="Brand Colors" description="Persisted and used for live previews.">
            <ColorPicker label="Primary Color" value={primary} defaultValue={DEFAULTS.primaryColor} onChange={changePrimary} />
            <ColorPicker label="Accent Color" value={accent} defaultValue={DEFAULTS.accentColor} onChange={changeAccent} />
            <div className="flex items-center gap-3 pt-1">
              <span className="text-sm text-neutral-500">Preview:</span>
              <span className="rounded-md px-3 py-1 text-sm font-medium text-white" style={{ backgroundColor: primary }}>
                Primary
              </span>
              <span className="rounded-md px-3 py-1 text-sm font-medium text-white" style={{ backgroundColor: accent }}>
                Accent
              </span>
            </div>
          </SettingsCard>

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

          <SettingsCard title="Layout">
            <Toggle
              label="Keep sidebar expanded"
              checked={sidebarExpanded}
              onChange={(v) => {
                setSidebarExpanded(v)
                setBool(PREF.sidebarExpanded, v)
              }}
            />
            <div>
              <span className="mb-1 block text-sm font-medium">Card spacing</span>
              <RadioRow<CardLayout>
                value={cardLayout}
                onChange={(v) => {
                  setCardLayout(v)
                  setString(PREF.cardLayout, v)
                }}
                options={[
                  { value: 'compact', label: 'Compact' },
                  { value: 'normal', label: 'Normal' },
                  { value: 'spacious', label: 'Spacious' },
                ]}
              />
            </div>
          </SettingsCard>
        </div>
      )}

      {tab === 'preferences' && (
        <div className="space-y-6">
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
            <div className="text-sm text-neutral-500">
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
            <div className="text-sm text-neutral-500">
              Preview: {format(now, dateFmtMap[dateFormat])}
            </div>
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
        </div>
      )}

      {tab === 'notifications' && isAdmin && <NotificationSettings />}

      {tab === 'about' && (
        <div className="space-y-6">
          <SettingsCard title="Application">
            <dl className="space-y-2 text-sm">
              <div className="flex justify-between"><dt className="text-neutral-500">Version</dt><dd className="font-medium">Sentinel v1.0</dd></div>
              <div className="flex justify-between"><dt className="text-neutral-500">License</dt><dd className="font-medium">MIT</dd></div>
              <div className="flex justify-between"><dt className="text-neutral-500">Frontend</dt><dd className="font-medium">React + TypeScript + Vite</dd></div>
              <div className="flex justify-between"><dt className="text-neutral-500">Database</dt><dd className="font-medium">PostgreSQL</dd></div>
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
              <button className="btn-secondary" onClick={() => push("You're on the latest version", 'info')}>
                Check for updates
              </button>
            </div>
          </SettingsCard>
        </div>
      )}

      {/* Footer actions */}
      <div className="flex items-center justify-between border-t border-neutral-200 pt-4 dark:border-neutral-800">
        <button className="btn-secondary text-error-600" onClick={() => setConfirmReset(true)}>
          Reset All to Defaults
        </button>
        <button className="btn-primary" onClick={saveAll}>
          Save Settings
        </button>
      </div>

      {confirmReset && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm p-6">
            <h3 className="text-lg font-semibold">Reset all settings?</h3>
            <p className="mt-2 text-sm text-neutral-500 dark:text-neutral-400">
              This restores every preference (theme, colors, font size, and more) to its default.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmReset(false)}>
                Cancel
              </button>
              <button className="btn bg-error-600 text-white hover:bg-error-700" onClick={doReset}>
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
