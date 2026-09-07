import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Check, X, Loader2, ArrowLeft } from 'lucide-react'
import { useAuth } from '@/hooks/useAuth'
import { validatePassword } from '@/utils/passwordValidator'

type Mode = 'login' | 'register'

const usernameRe = /^[a-zA-Z0-9_]{3,32}$/

function Card({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    // Same gradient ground as the app shell, so signing in and landing on the
    // dashboard read as one surface rather than two products.
    <div className="relative flex min-h-screen items-center justify-center bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 p-4">
      <div className="relative w-full max-w-sm rounded-xl border border-white/10 bg-slate-800/40 p-8 backdrop-blur-sm">
        {/* The dashboard's emerald-to-cyan wash, at card scale. */}
        <div className="pointer-events-none absolute inset-0 rounded-xl bg-gradient-to-r from-emerald-500/0 via-emerald-500/5 to-cyan-500/0" />
        <div className="relative z-10">
          <div className="mb-8 text-center">
            <h1 className="text-2xl font-light tracking-wide text-white">Sentinel</h1>
            <p className="mt-2 text-xs text-slate-400">Uptime Monitor</p>
          </div>
          <div className="mb-6 text-center">
            <h2 className="text-lg font-light text-white">{title}</h2>
            {subtitle && <p className="mt-1 text-sm text-slate-400">{subtitle}</p>}
          </div>
          {children}
        </div>
      </div>
    </div>
  )
}

const inputCls = 'rd-input px-3 py-2.5 text-sm'

function Req({ ok, label }: { ok: boolean; label: string }) {
  return (
    <div className={`flex items-center gap-1.5 text-xs ${ok ? 'text-primary-400' : 'text-slate-400'}`}>
      {ok ? <Check className="h-3.5 w-3.5" /> : <X className="h-3.5 w-3.5" />}
      {label}
    </div>
  )
}

export default function Auth({ mode }: { mode: Mode }) {
  const { login, register, verifyMFA, loading, error, setError } = useAuth()

  // Shared
  const usernameRef = useRef<HTMLInputElement>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  // Register
  const [confirm, setConfirm] = useState('')
  const [registered, setRegistered] = useState(false)

  // Login MFA
  const [mfaToken, setMfaToken] = useState<string | null>(null)
  const [totp, setTotp] = useState('')
  const [useBackup, setUseBackup] = useState(false)
  const [backup, setBackup] = useState('')

  // Whether the sign-up link/form should be offered. Registration may be closed
  // by an admin; the very first account (setup) is always allowed. null = still
  // loading, so we don't flash the link before we know.
  const [signupAllowed, setSignupAllowed] = useState<boolean | null>(null)

  useEffect(() => {
    if (mode === 'login') {
      const prefill = new URLSearchParams(window.location.search).get('username')
      if (prefill) setUsername(prefill)
    }
    usernameRef.current?.focus()
    setError(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode])

  // Fetch the public auth status to decide whether to offer registration.
  useEffect(() => {
    let active = true
    fetch('/api/v1/auth/status')
      .then((res) => res.json())
      .then((body) => {
        if (!active) return
        const data = body?.data ?? {}
        setSignupAllowed(Boolean(data.registration_enabled) || Boolean(data.setup_required))
      })
      .catch(() => active && setSignupAllowed(false))
    return () => {
      active = false
    }
  }, [])

  const strength = useMemo(() => validatePassword(password), [password])
  const usernameValid = usernameRe.test(username)
  const passwordsMatch = password.length > 0 && password === confirm
  const canRegister = usernameValid && strength.isStrong && passwordsMatch && !loading

  // ---- Handlers ----
  async function handleLogin(e: React.FormEvent) {
    e.preventDefault()
    if (!username || !password) return
    const result = await login(username, password)
    if (result.mfaRequired && result.mfaToken) {
      setMfaToken(result.mfaToken)
    } else if (!result.success) {
      setPassword('')
      usernameRef.current?.focus()
    }
  }

  async function handleRegister(e: React.FormEvent) {
    e.preventDefault()
    if (!canRegister) return
    const result = await register(username, password, confirm)
    if (result.success) {
      setRegistered(true)
      setTimeout(() => {
        window.location.href = `/login?username=${encodeURIComponent(username)}`
      }, 2000)
    }
  }

  async function handleVerifyTotp(e: React.FormEvent) {
    e.preventDefault()
    if (!mfaToken) return
    const result = await verifyMFA(mfaToken, totp)
    if (!result.success) setTotp('')
  }
  async function handleVerifyBackup(e: React.FormEvent) {
    e.preventDefault()
    if (!mfaToken) return
    const result = await verifyMFA(mfaToken, undefined, backup)
    if (!result.success) setBackup('')
  }

  // ---- MFA view ----
  if (mfaToken) {
    return (
      <Card title="Verify Your Identity">
        {!useBackup ? (
          <form onSubmit={handleVerifyTotp} className="space-y-4">
            <p className="text-sm text-slate-500">
              Enter the 6-digit code from your authenticator app.
            </p>
            <input
              autoFocus
              inputMode="numeric"
              maxLength={6}
              value={totp}
              onChange={(e) => setTotp(e.target.value.replace(/\D/g, ''))}
              className={`${inputCls} text-center text-lg tracking-[0.4em]`}
              placeholder="123456"
            />
            {error && <p className="text-sm text-error-600">{error}</p>}
            <button type="submit" className="btn-primary w-full" disabled={loading || totp.length !== 6}>
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Verify'}
            </button>
            <button
              type="button"
              className="w-full text-sm text-slate-500 hover:underline"
              onClick={() => {
                setUseBackup(true)
                setError(null)
              }}
            >
              Don't have your authenticator? Use a backup code
            </button>
          </form>
        ) : (
          <form onSubmit={handleVerifyBackup} className="space-y-4">
            <p className="text-sm text-slate-500">Enter one of your 8-character backup codes.</p>
            <input
              autoFocus
              maxLength={8}
              value={backup}
              onChange={(e) => setBackup(e.target.value.toUpperCase())}
              className={`${inputCls} text-center font-mono tracking-widest`}
              placeholder="ABCD2345"
            />
            {error && <p className="text-sm text-error-600">{error}</p>}
            <button type="submit" className="btn-primary w-full" disabled={loading || backup.length !== 8}>
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Verify Backup Code'}
            </button>
            <button
              type="button"
              className="flex w-full items-center justify-center gap-1 text-sm text-slate-500 hover:underline"
              onClick={() => {
                setUseBackup(false)
                setError(null)
              }}
            >
              <ArrowLeft className="h-3.5 w-3.5" /> Back to authenticator code
            </button>
          </form>
        )}
      </Card>
    )
  }

  // ---- Register view ----
  if (mode === 'register') {
    if (registered) {
      return (
        <Card title="Account created!">
          <p className="text-center text-sm text-slate-500">Redirecting to sign in…</p>
        </Card>
      )
    }
    // Registration explicitly closed (and setup already done): don't show the form.
    if (signupAllowed === false) {
      return (
        <Card title="Registration disabled">
          <p className="text-center text-sm text-slate-500">
            New account registration is currently disabled. Please contact an
            administrator for access.
          </p>
          <p className="mt-4 text-center text-sm text-slate-500">
            <Link to="/login" className="text-accent-400 hover:underline">
              Back to sign in
            </Link>
          </p>
        </Card>
      )
    }
    return (
      <Card title="Create Account">
        <form onSubmit={handleRegister} className="space-y-4">
          <div>
            <input
              ref={usernameRef}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className={inputCls}
              placeholder="Username"
            />
            <div className="mt-1 flex items-center justify-between text-xs">
              <span className={usernameValid ? 'text-primary-400' : 'text-slate-400'}>
                Letters, numbers, underscore
              </span>
              <span className="text-slate-400">{username.length}/32</span>
            </div>
          </div>

          <div>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className={inputCls}
              placeholder="Password"
            />
            <div className="mt-2 grid grid-cols-2 gap-1">
              <Req ok={strength.feedback.hasLength} label="12+ characters" />
              <Req ok={strength.feedback.hasUppercase} label="Uppercase letter" />
              <Req ok={strength.feedback.hasNumber} label="Number" />
              <Req ok={strength.feedback.hasSpecial} label="Special (!@#$%^&*)" />
            </div>
          </div>

          <div>
            <input
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              className={inputCls}
              placeholder="Confirm password"
            />
            {confirm.length > 0 && (
              <div className={`mt-1 text-xs ${passwordsMatch ? 'text-primary-400' : 'text-error-600'}`}>
                {passwordsMatch ? '✓ Passwords match' : "✗ Passwords don't match"}
              </div>
            )}
          </div>

          {error && <p className="text-sm text-error-600">{error}</p>}

          <button type="submit" className="btn-primary w-full" disabled={!canRegister}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Create Account'}
          </button>
        </form>
        <p className="mt-4 text-center text-sm text-slate-500">
          Already have an account?{' '}
          <Link to="/login" className="text-accent-400 hover:underline">
            Sign in
          </Link>
        </p>
      </Card>
    )
  }

  // ---- Login view ----
  return (
    <Card title="Sign In to Sentinel">
      <form onSubmit={handleLogin} className="space-y-4">
        <input
          ref={usernameRef}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          className={inputCls}
          placeholder="Username"
          autoComplete="username"
        />
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className={inputCls}
          placeholder="Password"
          autoComplete="current-password"
        />
        {error && <p className="text-sm text-error-600">{error}</p>}
        <button type="submit" className="btn-primary w-full" disabled={loading || !username || !password}>
          {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Sign In'}
        </button>
      </form>
      {signupAllowed && (
        <p className="mt-4 text-center text-sm text-slate-500">
          Don't have an account?{' '}
          <Link to="/register" className="text-accent-400 hover:underline">
            Sign up
          </Link>
        </p>
      )}
    </Card>
  )
}
