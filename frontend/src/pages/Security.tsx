import { useEffect, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import {
  Shield,
  ShieldCheck,
  ShieldOff,
  Copy,
  Download,
  Check,
  X,
  Monitor,
  UserCheck,
  UserX,
} from 'lucide-react'
import api, { type ApiError } from '@/services/api'
import { useAuthContext } from '@/context/AuthContext'
import { useToasts, Toaster } from '@/components/Toast'
import { validatePassword } from '@/utils/passwordValidator'

const inputCls =
  'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500'

function Req({ ok, label }: { ok: boolean; label: string }) {
  return (
    <div className={`flex items-center gap-1.5 text-xs ${ok ? 'text-emerald-600' : 'text-neutral-400'}`}>
      {ok ? <Check className="h-3.5 w-3.5" /> : <X className="h-3.5 w-3.5" />}
      {label}
    </div>
  )
}

export default function Security() {
  const { currentUser, getCurrentUser } = useAuthContext()
  const { toasts, push } = useToasts()
  const mfaEnabled = currentUser?.mfa_enabled ?? false
  const isAdmin = currentUser?.is_admin ?? false

  // ---- User registration (admin only) ----
  const [regEnabled, setRegEnabled] = useState<boolean | null>(null)
  const [regBusy, setRegBusy] = useState(false)

  useEffect(() => {
    if (!isAdmin) return
    let active = true
    api
      .get<{ data: { registration_enabled: boolean } }>('/settings')
      .then((res) => active && setRegEnabled(res.data.data.registration_enabled))
      .catch(() => active && setRegEnabled(null))
    return () => {
      active = false
    }
  }, [isAdmin])

  const toggleRegistration = async (next: boolean) => {
    setRegBusy(true)
    try {
      await api.patch('/settings/registration', { enabled: next })
      setRegEnabled(next)
      push('Registration settings updated', 'success')
    } catch (err) {
      push((err as ApiError).message || 'Failed to update registration setting', 'error')
    } finally {
      setRegBusy(false)
    }
  }

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

  const copyCodes = () => {
    void navigator.clipboard.writeText(backupCodes.join('\n'))
    push('Backup codes copied', 'info')
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

  return (
    <div id="security" className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Security</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Manage your account security and authentication
        </p>
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
                ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                : 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
            }`}
          >
            {mfaEnabled ? <ShieldCheck className="h-3.5 w-3.5" /> : <ShieldOff className="h-3.5 w-3.5" />}
            {mfaEnabled ? 'Enabled' : 'Disabled'}
          </span>
        </div>

        {mfaEnabled ? (
          <>
            <p className="text-sm text-neutral-500 dark:text-neutral-400">
              Your account is protected with two-factor authentication.
            </p>
            <button
              className="btn border border-error-300 text-error-600 hover:bg-error-50 dark:hover:bg-error-900/20"
              onClick={() => setDisableOpen(true)}
            >
              <ShieldOff className="h-4 w-4" /> Disable Two-Factor Authentication
            </button>
          </>
        ) : (
          <>
            <p className="text-sm text-neutral-500 dark:text-neutral-400">
              Add an extra layer of security to your account with an authenticator app.
            </p>
            <button className="btn-primary" disabled={busy} onClick={() => void startEnable()}>
              <Shield className="h-4 w-4" /> Enable Two-Factor Authentication
            </button>
          </>
        )}
      </div>

      {/* User registration (admin only) */}
      {isAdmin && (
        <div className="card space-y-4 p-5">
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">User Registration</h2>
            <span
              className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium ${
                regEnabled
                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                  : 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
              }`}
            >
              {regEnabled ? <UserCheck className="h-3.5 w-3.5" /> : <UserX className="h-3.5 w-3.5" />}
              {regEnabled === null ? 'Loading…' : regEnabled ? 'Enabled' : 'Disabled'}
            </span>
          </div>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Control whether new users can create accounts.
          </p>
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="checkbox"
              className="h-4 w-4 rounded border-neutral-300 text-primary-600 focus:ring-primary-500"
              checked={!!regEnabled}
              disabled={regEnabled === null || regBusy}
              onChange={(e) => void toggleRegistration(e.target.checked)}
            />
            <span className="text-sm">Allow user registration</span>
          </label>
          {regEnabled === false && (
            <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-200">
              ⚠️ Disabled — only you and explicitly invited users can access Sentinel.
            </div>
          )}
        </div>
      )}

      {/* Sessions */}
      <div className="card space-y-3 p-5">
        <h2 className="font-semibold">Sessions</h2>
        <div className="flex items-center gap-3 rounded-md border border-neutral-200 p-3 dark:border-neutral-800">
          <Monitor className="h-5 w-5 text-neutral-400" />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">
              Current Device{' '}
              <span className="ml-1 rounded bg-primary-100 px-1.5 py-0.5 text-xs text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                active
              </span>
            </div>
            <div className="truncate text-xs text-neutral-500">{navigator.userAgent}</div>
          </div>
        </div>
        <p className="text-xs text-neutral-500">
          Tokens are stateless; signing in elsewhere issues a separate token.
        </p>
      </div>

      {/* MFA enable modal */}
      {step !== 'idle' && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-md space-y-4 p-6">
            {step === 'scan' && (
              <>
                <h3 className="text-lg font-semibold">Set up authenticator app</h3>
                <p className="text-sm text-neutral-500">
                  Scan this QR code with Google Authenticator, Authy, or a similar app.
                </p>
                <div className="flex justify-center rounded-md bg-white p-4">
                  <QRCodeSVG value={qrUrl} size={180} />
                </div>
                <div className="text-center text-xs text-neutral-500">
                  Can't scan? Enter this code:
                  <div className="mt-1 break-all font-mono text-sm text-neutral-700 dark:text-neutral-300">
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
                <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-200">
                  ⚠️ Store these in a safe place. Each code works once if you lose your authenticator.
                </div>
                <div className="grid grid-cols-2 gap-2 rounded-md bg-neutral-100 p-3 font-mono text-sm dark:bg-neutral-800">
                  {backupCodes.map((c) => (
                    <span key={c}>{c}</span>
                  ))}
                </div>
                <div className="flex gap-2">
                  <button className="btn-secondary !py-1" onClick={copyCodes}>
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
            <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-200">
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

      <Toaster toasts={toasts} />
    </div>
  )
}
