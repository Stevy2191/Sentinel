import { useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import { format } from 'date-fns'
import {
  Shield,
  ShieldCheck,
  ShieldOff,
  Copy,
  Download,
  Check,
  X,
  Monitor,
  User,
  Volume2,
} from 'lucide-react'
import api, { type ApiError } from '@/services/api'
import { useAuthContext } from '@/context/AuthContext'
import { useToasts, Toaster } from '@/components/Toast'
import { validatePassword } from '@/utils/passwordValidator'
import SettingsCard from '@/components/SettingsCard'
import {
  PREF,
  DEFAULTS,
  applyStoredPreferences,
  resetAllPreferences,
  getString,
  getBool,
  setString,
  setBool,
  type FontSize,
  type TimeFormat,
  type DateFormatPref,
  type ReportRange,
} from '@/utils/preferences'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

const dateFmtMap: Record<DateFormatPref, string> = {
  'MMM DD, YYYY': 'MMM dd, yyyy',
  'DD/MM/YYYY': 'dd/MM/yyyy',
  'YYYY-MM-DD': 'yyyy-MM-dd',
}

function Req({ ok, label }: { ok: boolean; label: string }) {
  return (
    <div className={`flex items-center gap-1.5 text-xs ${ok ? 'text-emerald-600' : 'text-slate-400'}`}>
      {ok ? <Check className="h-3.5 w-3.5" /> : <X className="h-3.5 w-3.5" />}
      {label}
    </div>
  )
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

// legacyCopy copies text using a temporary textarea + document.execCommand, the
// only clipboard mechanism available outside a secure context (plain HTTP).
// Returns whether the copy succeeded.
function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.top = '0'
  ta.style.left = '0'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  document.body.removeChild(ta)
  return ok
}

export default function Profile() {
  const { currentUser, getCurrentUser } = useAuthContext()
  const { toasts, push } = useToasts()
  const mfaEnabled = currentUser?.mfa_enabled ?? false
  const isAdmin = currentUser?.is_admin ?? false

  // ---- Change password ----
  const [curPw, setCurPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [pwLoading, setPwLoading] = useState(false)
  const strength = validatePassword(newPw)
  const pwMatch = newPw.length > 0 && newPw === confirmPw
  const canChangePw = curPw.length > 0 && strength.isStrong && pwMatch && !pwLoading

  const changePassword = async () => {
    setPwLoading(true)
    try {
      await api.post('/auth/password', { current_password: curPw, new_password: newPw })
      push('Password updated successfully', 'success')
      setCurPw('')
      setNewPw('')
      setConfirmPw('')
    } catch (err) {
      push((err as ApiError).message || 'Failed to update password', 'error')
    } finally {
      setPwLoading(false)
    }
  }

  // ---- MFA enable flow ----
  const [step, setStep] = useState<'idle' | 'scan' | 'codes'>('idle')
  const [secret, setSecret] = useState('')
  const [qrUrl, setQrUrl] = useState('')
  const [backupCodes, setBackupCodes] = useState<string[]>([])
  const [setupTotp, setSetupTotp] = useState('')
  const [ack, setAck] = useState(false)
  const [busy, setBusy] = useState(false)

  const startEnable = async () => {
    setBusy(true)
    try {
      const { data } = await api.post<{
        data: { secret: string; qr_code_url: string; backup_codes: string[] }
      }>('/auth/mfa/setup')
      setSecret(data.data.secret)
      setQrUrl(data.data.qr_code_url)
      setBackupCodes(data.data.backup_codes)
      setStep('scan')
    } catch (err) {
      push((err as ApiError).message || 'Failed to start MFA setup', 'error')
    } finally {
      setBusy(false)
    }
  }

  const confirmEnable = async () => {
    setBusy(true)
    try {
      await api.post('/auth/mfa/confirm', { totp_code: setupTotp })
      setStep('codes')
    } catch (err) {
      push((err as ApiError).message || 'Invalid code', 'error')
      setSetupTotp('')
    } finally {
      setBusy(false)
    }
  }

  const finishEnable = async () => {
    setStep('idle')
    setSetupTotp('')
    setAck(false)
    await getCurrentUser()
    push('Two-Factor Authentication enabled', 'success')
  }

  const copyCodes = async () => {
    const text = backupCodes.join('\n')
    // navigator.clipboard is only available in a secure context (HTTPS or
    // localhost). Self-hosted Sentinel is often served over plain HTTP, where it
    // is undefined and the old call threw silently — hence "nothing happens".
    try {
      if (window.isSecureContext && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
        push('Backup codes copied', 'success')
        return
      }
    } catch {
      // Fall through to the legacy execCommand path below.
    }
    if (legacyCopy(text)) {
      push('Backup codes copied', 'success')
    } else {
      push('Could not copy — select the codes and copy manually', 'error')
    }
  }
  const downloadCodes = () => {
    const blob = new Blob([backupCodes.join('\n') + '\n'], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'sentinel-backup-codes.txt'
    a.click()
    URL.revokeObjectURL(url)
  }

  // ---- MFA disable flow ----
  const [disableOpen, setDisableOpen] = useState(false)
  const [disableTotp, setDisableTotp] = useState('')

  const disableMFA = async () => {
    setBusy(true)
    try {
      await api.post('/auth/mfa/disable', { totp_code: disableTotp })
      setDisableOpen(false)
      setDisableTotp('')
      await getCurrentUser()
      push('Two-Factor Authentication disabled', 'success')
    } catch (err) {
      push((err as ApiError).message || 'Invalid code', 'error')
    } finally {
      setBusy(false)
    }
  }

  // ---- Preferences (this browser) ----
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
    setDateFormat(DEFAULTS.dateFormat)
    setReportRange(DEFAULTS.reportRange)
    applyStoredPreferences()
    setConfirmReset(false)
    push('Preferences reset to defaults', 'success')
  }

  const now = new Date()

  return (
    <div id="profile" className="max-w-2xl space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Profile</h1>
        <p className="text-sm text-slate-400">Your account and how you sign in</p>
      </div>

      {/* Who you are. Read-only: a username is an identity other records point
          at, so it is changed by an admin on the Users page, not here. */}
      <div className="card flex items-center gap-4 p-5">
        <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-emerald-500 text-xl font-semibold text-slate-900">
          {(currentUser?.username ?? '?').charAt(0).toUpperCase()}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-lg font-medium text-white">
            {currentUser?.username ?? '\u2014'}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-400">
            <span className="inline-flex items-center gap-1">
              <User className="h-3.5 w-3.5" />
              {isAdmin ? 'Administrator' : 'Member'}
            </span>
            <span className={mfaEnabled ? 'text-emerald-400' : 'text-slate-400'}>
              {mfaEnabled ? 'Two-factor on' : 'Two-factor off'}
            </span>
            {currentUser?.last_login && (
              <span>Last signed in {new Date(currentUser.last_login).toLocaleString()}</span>
            )}
          </div>
        </div>
      </div>

      {/* Change password */}
      <div className="card space-y-4 p-5">
        <h2 className="font-semibold">Change Password</h2>
        <input
          type="password"
          className={inputCls}
          placeholder="Current password"
          value={curPw}
          onChange={(e) => setCurPw(e.target.value)}
          autoComplete="current-password"
        />
        <div>
          <input
            type="password"
            className={inputCls}
            placeholder="New password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            autoComplete="new-password"
          />
          {newPw.length > 0 && (
            <div className="mt-2 grid grid-cols-2 gap-1">
              <Req ok={strength.feedback.hasLength} label="12+ characters" />
              <Req ok={strength.feedback.hasUppercase} label="Uppercase letter" />
              <Req ok={strength.feedback.hasNumber} label="Number" />
              <Req ok={strength.feedback.hasSpecial} label="Special (!@#$%^&*)" />
            </div>
          )}
        </div>
        <div>
          <input
            type="password"
            className={inputCls}
            placeholder="Confirm new password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
            autoComplete="new-password"
          />
          {confirmPw.length > 0 && (
            <div className={`mt-1 text-xs ${pwMatch ? 'text-emerald-600' : 'text-error-600'}`}>
              {pwMatch ? '✓ Passwords match' : "✗ Passwords don't match"}
            </div>
          )}
        </div>
        <button className="btn-primary" disabled={!canChangePw} onClick={() => void changePassword()}>
          {pwLoading ? 'Updating…' : 'Update Password'}
        </button>
      </div>

      {/* Two-factor authentication */}
      <div className="card space-y-4 p-5">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">Two-Factor Authentication</h2>
          <span
            className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium ${
              mfaEnabled
                ? 'bg-emerald-500/20 text-emerald-400'
                : 'bg-red-500/20 text-red-400'
            }`}
          >
            {mfaEnabled ? <ShieldCheck className="h-3.5 w-3.5" /> : <ShieldOff className="h-3.5 w-3.5" />}
            {mfaEnabled ? 'Enabled' : 'Disabled'}
          </span>
        </div>

        {mfaEnabled ? (
          <>
            <p className="text-sm text-slate-400">
              Your account is protected with two-factor authentication.
            </p>
            <button
              className="btn border border-red-500/30 text-red-400 hover:bg-red-500/10"
              onClick={() => setDisableOpen(true)}
            >
              <ShieldOff className="h-4 w-4" /> Disable Two-Factor Authentication
            </button>
          </>
        ) : (
          <>
            <p className="text-sm text-slate-400">
              Add an extra layer of security to your account with an authenticator app.
            </p>
            <button className="btn-primary" disabled={busy} onClick={() => void startEnable()}>
              <Shield className="h-4 w-4" /> Enable Two-Factor Authentication
            </button>
          </>
        )}
      </div>

      {/* Sessions */}
      <div className="card space-y-3 p-5">
        <h2 className="font-semibold">Sessions</h2>
        <div className="flex items-center gap-3 rounded-md border border-white/10 p-3">
          <Monitor className="h-5 w-5 text-slate-400" />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">
              Current Device{' '}
              <span className="ml-1 rounded bg-primary-500/20 px-1.5 py-0.5 text-xs text-primary-300">
                active
              </span>
            </div>
            <div className="truncate text-xs text-slate-500">{navigator.userAgent}</div>
          </div>
        </div>
        <p className="text-xs text-slate-500">
          Tokens are stateless; signing in elsewhere issues a separate token.
        </p>
      </div>

      {/* Preferences — stored in this browser and affect only you. */}
      <div>
        <h2 className="text-lg font-semibold">Preferences</h2>
        <p className="text-sm text-slate-400">These are stored in this browser and affect only you.</p>
      </div>

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

      {/* Scoped to preferences: these buttons only touch browser-local state. */}
      <div className="flex items-center justify-between border-t border-white/10 pt-4">
        <button className="btn-secondary text-red-400" onClick={() => setConfirmReset(true)}>
          Reset to Defaults
        </button>
        <button className="btn-primary" onClick={savePreferences}>
          Save Preferences
        </button>
      </div>

      {/* MFA enable modal */}
      {step !== 'idle' && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-md space-y-4 p-6">
            {step === 'scan' && (
              <>
                <h3 className="text-lg font-semibold">Set up authenticator app</h3>
                <p className="text-sm text-slate-500">
                  Scan this QR code with Google Authenticator, Authy, or a similar app.
                </p>
                <div className="flex justify-center rounded-md bg-white p-4">
                  <QRCodeSVG value={qrUrl} size={180} />
                </div>
                <div className="text-center text-xs text-slate-500">
                  Can't scan? Enter this code:
                  <div className="mt-1 break-all font-mono text-sm text-slate-300">
                    {secret}
                  </div>
                </div>
                <input
                  autoFocus
                  inputMode="numeric"
                  maxLength={6}
                  className={`${inputCls} text-center text-lg tracking-[0.4em]`}
                  placeholder="123456"
                  value={setupTotp}
                  onChange={(e) => setSetupTotp(e.target.value.replace(/\D/g, ''))}
                />
                <div className="flex justify-end gap-2">
                  <button className="btn-secondary" onClick={() => setStep('idle')}>
                    Cancel
                  </button>
                  <button
                    className="btn-primary"
                    disabled={busy || setupTotp.length !== 6}
                    onClick={() => void confirmEnable()}
                  >
                    Verify &amp; Enable
                  </button>
                </div>
              </>
            )}

            {step === 'codes' && (
              <>
                <h3 className="text-lg font-semibold">Save your backup codes</h3>
                <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-300">
                  ⚠️ Store these in a safe place. Each code works once if you lose your authenticator.
                </div>
                <div className="grid grid-cols-2 gap-2 rounded-md bg-white/5 p-3 font-mono text-sm">
                  {backupCodes.map((c) => (
                    <span key={c}>{c}</span>
                  ))}
                </div>
                <div className="flex gap-2">
                  <button className="btn-secondary !py-1" onClick={() => void copyCodes()}>
                    <Copy className="h-4 w-4" /> Copy
                  </button>
                  <button className="btn-secondary !py-1" onClick={downloadCodes}>
                    <Download className="h-4 w-4" /> Download
                  </button>
                </div>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
                  I've saved my backup codes
                </label>
                <div className="flex justify-end">
                  <button className="btn-primary" disabled={!ack} onClick={() => void finishEnable()}>
                    Done
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {/* MFA disable modal */}
      {disableOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm space-y-4 p-6">
            <h3 className="text-lg font-semibold">Disable two-factor authentication</h3>
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-300">
              ⚠️ This removes the extra security from your account.
            </div>
            <input
              autoFocus
              inputMode="numeric"
              maxLength={6}
              className={`${inputCls} text-center text-lg tracking-[0.4em]`}
              placeholder="123456"
              value={disableTotp}
              onChange={(e) => setDisableTotp(e.target.value.replace(/\D/g, ''))}
            />
            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setDisableOpen(false)}>
                Cancel
              </button>
              <button
                className="btn bg-error-600 text-white hover:bg-error-700"
                disabled={busy || disableTotp.length !== 6}
                onClick={() => void disableMFA()}
              >
                Disable
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Reset preferences modal */}
      {confirmReset && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm p-6">
            <h3 className="text-lg font-semibold">Reset preferences?</h3>
            <p className="mt-2 text-sm text-slate-400">
              This restores your browser preferences — font size, sound, time and date format — to
              their defaults. Account and system settings are not affected.
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
