import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams, Link } from 'react-router-dom'
import { ShieldCheck, Check, X, Loader2, Eye, EyeOff } from 'lucide-react'
import { format } from 'date-fns'
import { useAuthContext } from '@/context/AuthContext'
import { validatePassword } from '@/utils/passwordValidator'
import { useInvitationDetails, useAcceptInvitation } from '@/hooks/useInvitationAccept'

const usernameRe = /^[a-zA-Z0-9_]{3,32}$/
const inputCls =
  'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500'

function Shell({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-neutral-100 p-4 dark:bg-neutral-950">
      <div className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-6 shadow-card dark:border-neutral-800 dark:bg-neutral-900">
        <div className="mb-6 text-center">
          <div className="mb-2 flex items-center justify-center gap-2">
            <ShieldCheck className="h-8 w-8 text-primary-600" />
            <span className="text-xl font-bold">Sentinel</span>
          </div>
          <h1 className="text-lg font-semibold">{title}</h1>
          {subtitle && <p className="text-sm text-neutral-500">{subtitle}</p>}
        </div>
        {children}
      </div>
    </div>
  )
}

function Req({ ok, label }: { ok: boolean; label: string }) {
  return (
    <div className={`flex items-center gap-1.5 text-xs ${ok ? 'text-emerald-600' : 'text-neutral-400'}`}>
      {ok ? <Check className="h-3.5 w-3.5" /> : <X className="h-3.5 w-3.5" />}
      {label}
    </div>
  )
}

function RoleBadge({ role }: { role: 'admin' | 'user' }) {
  return role === 'admin' ? (
    <span className="rounded px-2 py-0.5 text-xs font-medium bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
      Admin
    </span>
  ) : (
    <span className="rounded px-2 py-0.5 text-xs font-medium bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
      User
    </span>
  )
}

const STRENGTH = ['Weak', 'Weak', 'Weak', 'Fair', 'Strong'] as const
const STRENGTH_COLOR = ['bg-red-500', 'bg-red-500', 'bg-red-500', 'bg-amber-500', 'bg-emerald-500'] as const

export default function InvitationAccept() {
  const { token } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const { isAuthenticated, setToken } = useAuthContext()
  const { invitation, loading, error } = useInvitationDetails(token)
  const { accept, loading: submitting } = useAcceptInvitation()

  // Already signed in — nothing to accept here.
  useEffect(() => {
    if (isAuthenticated) navigate('/dashboard')
  }, [isAuthenticated, navigate])

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [showPw, setShowPw] = useState(false)
  const [touched, setTouched] = useState<{ u?: boolean; p?: boolean; c?: boolean }>({})
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  const strength = useMemo(() => validatePassword(password), [password])
  const usernameValid = usernameRe.test(username)
  const passwordsMatch = confirm.length > 0 && password === confirm
  const canSubmit = usernameValid && strength.isStrong && passwordsMatch && !submitting

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setTouched({ u: true, p: true, c: true })
    if (!canSubmit || !token) return
    setSubmitError(null)
    try {
      const account = await accept(token, username, password)
      setDone(true)
      setToken(account.token) // auto-login
      window.setTimeout(() => navigate('/dashboard'), 1000)
    } catch (err) {
      const msg = (err as { message?: string }).message ?? 'Failed to accept invitation'
      setSubmitError(msg)
      if (/username/i.test(msg)) {
        setUsername('')
        setTouched((t) => ({ ...t, u: false }))
      }
    }
  }

  // ---- loading / error / expired / accepted states ----
  if (loading) {
    return (
      <Shell title="Loading invitation…">
        <div className="flex justify-center py-6 text-neutral-400">
          <Loader2 className="h-8 w-8 animate-spin" />
        </div>
      </Shell>
    )
  }
  if (error || !invitation) {
    return (
      <Shell title="Invalid invitation" subtitle={error ?? 'This invitation link is invalid.'}>
        <div className="text-center text-sm">
          <Link to="/login" className="text-primary-600 hover:underline">
            Go to sign in →
          </Link>
        </div>
      </Shell>
    )
  }
  if (invitation.accepted || invitation.expired) {
    return (
      <Shell
        title={invitation.expired ? 'Invitation expired' : 'Invitation already used'}
        subtitle={
          invitation.expired
            ? 'This link has expired. Ask your admin for a new invitation.'
            : 'This invitation has already been accepted.'
        }
      >
        <div className="text-center text-sm">
          <Link to="/login" className="text-primary-600 hover:underline">
            Go to sign in →
          </Link>
        </div>
      </Shell>
    )
  }
  if (done) {
    return (
      <Shell title="Account created!" subtitle="Signing you in…">
        <div className="flex justify-center py-4 text-emerald-600">
          <Loader2 className="h-8 w-8 animate-spin" />
        </div>
      </Shell>
    )
  }

  // ---- valid invitation → signup form ----
  const expiresSoon = new Date(invitation.expires_at).getTime() - Date.now() < 24 * 3600e3

  return (
    <Shell title="You're invited to Sentinel" subtitle="Create your account to join">
      {/* Invitation details */}
      <div className="mb-5 space-y-2 rounded-md border border-neutral-200 bg-neutral-50 p-3 text-sm dark:border-neutral-800 dark:bg-neutral-800/50">
        <div className="flex items-center justify-between">
          <span className="text-neutral-500 dark:text-neutral-400">Email</span>
          <span className="font-medium">{invitation.email}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-neutral-500 dark:text-neutral-400">Role</span>
          <RoleBadge role={invitation.role} />
        </div>
        <div className="flex items-center justify-between">
          <span className="text-neutral-500 dark:text-neutral-400">Expires</span>
          <span className={expiresSoon ? 'text-red-500' : ''}>
            {format(new Date(invitation.expires_at), 'MMM d, yyyy')}
          </span>
        </div>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        <div>
          <input
            autoFocus
            className={inputCls}
            placeholder="Username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            onBlur={() => setTouched((t) => ({ ...t, u: true }))}
          />
          <div className="mt-1 flex items-center justify-between text-xs">
            <span className={usernameValid ? 'text-emerald-600' : 'text-neutral-400'}>
              Letters, numbers, underscore (3–32)
            </span>
            <span className="text-neutral-400">{username.length}/32</span>
          </div>
          {touched.u && username.length > 0 && !usernameValid && (
            <p className="mt-1 text-xs text-error-600">Invalid username</p>
          )}
        </div>

        <div>
          <div className="relative">
            <input
              type={showPw ? 'text' : 'password'}
              className={`${inputCls} pr-10`}
              placeholder="Password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onBlur={() => setTouched((t) => ({ ...t, p: true }))}
            />
            <button
              type="button"
              onClick={() => setShowPw((s) => !s)}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-neutral-400 hover:text-neutral-600"
              tabIndex={-1}
              aria-label={showPw ? 'Hide password' : 'Show password'}
            >
              {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </div>
          {password.length > 0 && (
            <div className="mt-2">
              <div className="mb-1 flex gap-1">
                <div className={`h-1 flex-1 rounded ${strength.score >= 1 ? STRENGTH_COLOR[strength.score] : 'bg-neutral-200 dark:bg-neutral-700'}`} />
                <div className={`h-1 flex-1 rounded ${strength.score >= 3 ? STRENGTH_COLOR[strength.score] : 'bg-neutral-200 dark:bg-neutral-700'}`} />
                <div className={`h-1 flex-1 rounded ${strength.score >= 4 ? STRENGTH_COLOR[strength.score] : 'bg-neutral-200 dark:bg-neutral-700'}`} />
              </div>
              <p className="text-xs text-neutral-500">Strength: {STRENGTH[strength.score]}</p>
            </div>
          )}
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
            className={inputCls}
            placeholder="Confirm password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            onBlur={() => setTouched((t) => ({ ...t, c: true }))}
          />
          {confirm.length > 0 && (
            <div className={`mt-1 text-xs ${passwordsMatch ? 'text-emerald-600' : 'text-error-600'}`}>
              {passwordsMatch ? '✓ Passwords match' : "✗ Passwords don't match"}
            </div>
          )}
        </div>

        {submitError && <p className="text-sm text-error-600">{submitError}</p>}

        <button type="submit" className="btn-primary w-full" disabled={!canSubmit}>
          {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Accept Invitation'}
        </button>
        <button type="button" className="btn-secondary w-full" onClick={() => navigate('/login')}>
          Cancel
        </button>
      </form>

      <p className="mt-4 text-center text-sm text-neutral-500">
        Already have an account?{' '}
        <Link to="/login" className="text-primary-600 hover:underline">
          Sign in
        </Link>
      </p>
    </Shell>
  )
}
