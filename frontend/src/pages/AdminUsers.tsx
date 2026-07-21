import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowUpDown, Copy, Trash2, Shield, Send, Mail, Link as LinkIcon, X } from 'lucide-react'
import { format } from 'date-fns'
import { useAuthContext } from '@/context/AuthContext'
import { useToasts, Toaster } from '@/components/Toast'
import {
  useUsers,
  usePendingInvitations,
  useCreateUser,
  useCreateUserAutoPassword,
  useResetPassword,
  useResetPasswordAuto,
  useChangeUserRole,
  useDeleteUser,
  useInviteUser,
  useResendInvitation,
  useCancelInvitation,
  invitationLink,
  type ManagedUser,
  type Role,
} from '@/hooks/useUserManagement'

const inputCls =
  'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500'
const PER_PAGE = 10

async function copy(text: string, push: (m: string, t?: 'success' | 'error' | 'info') => void) {
  try {
    if (window.isSecureContext && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    push('Copied to clipboard', 'success')
  } catch {
    push('Could not copy — copy it manually', 'error')
  }
}

function RoleBadge({ role }: { role: Role }) {
  return role === 'admin' ? (
    <span className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
      <Shield className="h-3 w-3" /> Admin
    </span>
  ) : (
    <span className="rounded px-1.5 py-0.5 text-xs font-medium bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
      User
    </span>
  )
}

type SortKey = 'username' | 'email' | 'role' | 'created_at'
type Modal =
  | { kind: 'password'; title: string; username: string; password: string }
  | { kind: 'resetPw'; user: ManagedUser }
  | { kind: 'confirmDelete'; user: ManagedUser }
  | { kind: 'confirmRole'; user: ManagedUser; to: Role }
  | { kind: 'invitationLink'; email: string; link: string }
  | null

export default function AdminUsers() {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const { toasts, push } = useToasts()
  const isAdmin = currentUser?.is_admin ?? false

  const { users, refetch: refetchUsers } = useUsers()
  const { invitations, refetch: refetchInvites } = usePendingInvitations()
  const { create: createUser } = useCreateUser()
  const { create: createUserAuto } = useCreateUserAutoPassword()
  const { reset: resetPw } = useResetPassword()
  const { reset: resetPwAuto } = useResetPasswordAuto()
  const { change: changeRole } = useChangeUserRole()
  const { remove: deleteUser } = useDeleteUser()
  const { invite } = useInviteUser()
  const { resend } = useResendInvitation()
  const { cancel } = useCancelInvitation()

  // Redirect non-admins away.
  useEffect(() => {
    if (currentUser && !isAdmin) navigate('/dashboard')
  }, [currentUser, isAdmin, navigate])

  const [tab, setTab] = useState<'create' | 'invite' | 'pending'>('create')
  const [modal, setModal] = useState<Modal>(null)

  // ---- users list state ----
  const [search, setSearch] = useState('')
  const [roleFilter, setRoleFilter] = useState<'all' | Role>('all')
  const [sortKey, setSortKey] = useState<SortKey>('created_at')
  const [sortAsc, setSortAsc] = useState(false)
  const [page, setPage] = useState(1)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    const rows = users.filter(
      (u) =>
        (roleFilter === 'all' || u.role === roleFilter) &&
        (!q || u.username.toLowerCase().includes(q) || u.email.toLowerCase().includes(q))
    )
    rows.sort((a, b) => {
      const av = String(a[sortKey] ?? '').toLowerCase()
      const bv = String(b[sortKey] ?? '').toLowerCase()
      return (av < bv ? -1 : av > bv ? 1 : 0) * (sortAsc ? 1 : -1)
    })
    return rows
  }, [users, search, roleFilter, sortKey, sortAsc])

  const totalPages = Math.max(1, Math.ceil(filtered.length / PER_PAGE))
  const safePage = Math.min(page, totalPages)
  const pageRows = filtered.slice((safePage - 1) * PER_PAGE, safePage * PER_PAGE)
  const toggleSort = (k: SortKey) => {
    if (sortKey === k) setSortAsc((s) => !s)
    else {
      setSortKey(k)
      setSortAsc(true)
    }
  }

  if (!currentUser || !isAdmin) return null

  // ---- actions ----
  const doChangeRole = async (u: ManagedUser, to: Role) => {
    try {
      await changeRole(u.id, to)
      push(`${u.username} is now ${to === 'admin' ? 'an admin' : 'a user'}`, 'success')
      setModal(null)
      await refetchUsers()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to change role', 'error')
    }
  }
  const doDelete = async (u: ManagedUser) => {
    try {
      await deleteUser(u.id)
      push(`User ${u.username} deleted`, 'success')
      setModal(null)
      await refetchUsers()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to delete user', 'error')
    }
  }

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">User Management</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">Create, invite, and manage users</p>
      </div>

      {/* ---- Users list ---- */}
      <div className="card p-4">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <input
            className={`${inputCls} max-w-xs`}
            placeholder="Search username or email…"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              setPage(1)
            }}
          />
          <select
            className="rounded-md border border-neutral-300 bg-white px-2.5 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800"
            value={roleFilter}
            onChange={(e) => {
              setRoleFilter(e.target.value as 'all' | Role)
              setPage(1)
            }}
          >
            <option value="all">All roles</option>
            <option value="admin">Admins</option>
            <option value="user">Users</option>
          </select>
          <span className="ml-auto text-sm text-neutral-500">{filtered.length} user(s)</span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-200 text-neutral-500 dark:border-neutral-800 dark:text-neutral-400">
              <tr>
                {(['username', 'email', 'role', 'created_at'] as SortKey[]).map((k) => (
                  <th key={k} className="px-3 py-2 font-medium">
                    <button className="flex items-center gap-1 capitalize" onClick={() => toggleSort(k)}>
                      {k === 'created_at' ? 'Created' : k} <ArrowUpDown className="h-3 w-3" />
                    </button>
                  </th>
                ))}
                <th className="px-3 py-2 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-100 dark:divide-neutral-800">
              {pageRows.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-3 py-8 text-center text-neutral-500">
                    No users match.
                  </td>
                </tr>
              ) : (
                pageRows.map((u) => {
                  const isSelf = u.id === currentUser.user_id
                  return (
                    <tr key={u.id} className="hover:bg-neutral-50 dark:hover:bg-neutral-800/50">
                      <td className="px-3 py-2 font-medium">
                        {u.username}
                        {isSelf && <span className="ml-2 text-xs italic text-neutral-400">you</span>}
                      </td>
                      <td className="px-3 py-2 text-neutral-500">{u.email || '—'}</td>
                      <td className="px-3 py-2">
                        <RoleBadge role={u.role} />
                      </td>
                      <td className="px-3 py-2 text-neutral-500">
                        {u.created_at ? format(new Date(u.created_at), 'MMM d, yyyy') : '—'}
                      </td>
                      <td className="px-3 py-2">
                        <div className="flex flex-wrap justify-end gap-1.5">
                          {!isSelf &&
                            (u.role === 'user' ? (
                              <button className="btn-secondary !py-1 !px-2 text-emerald-600" onClick={() => setModal({ kind: 'confirmRole', user: u, to: 'admin' })}>
                                Make Admin
                              </button>
                            ) : (
                              <button className="btn-secondary !py-1 !px-2" onClick={() => setModal({ kind: 'confirmRole', user: u, to: 'user' })}>
                                Make User
                              </button>
                            ))}
                          {!isSelf && (
                            <button className="btn-secondary !py-1 !px-2" onClick={() => setModal({ kind: 'resetPw', user: u })}>
                              Reset Password
                            </button>
                          )}
                          {!isSelf && (
                            <button
                              className="btn-secondary !py-1 !px-2 text-error-600"
                              title="Delete user"
                              onClick={() => setModal({ kind: 'confirmDelete', user: u })}
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>

        <div className="mt-3 flex items-center justify-between text-sm text-neutral-500">
          <span>Page {safePage} of {totalPages}</span>
          <div className="flex gap-2">
            <button className="btn-secondary !py-1" disabled={safePage <= 1} onClick={() => setPage(safePage - 1)}>
              Previous
            </button>
            <button className="btn-secondary !py-1" disabled={safePage >= totalPages} onClick={() => setPage(safePage + 1)}>
              Next
            </button>
          </div>
        </div>
      </div>

      {/* ---- Quick actions (tabs) ---- */}
      <div className="card p-4">
        <div className="mb-4 flex flex-wrap gap-2">
          {(['create', 'invite', 'pending'] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`rounded-md px-4 py-2 text-sm font-medium ${
                tab === t ? 'bg-primary-600 text-white' : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
              }`}
            >
              {t === 'create' ? 'Create User' : t === 'invite' ? 'Invite User' : `Pending (${invitations.length})`}
            </button>
          ))}
        </div>

        {tab === 'create' && (
          <CreateUserTab
            onCreated={refetchUsers}
            push={push}
            createUser={createUser}
            createUserAuto={createUserAuto}
            showPassword={(username, password) => setModal({ kind: 'password', title: 'User created', username, password })}
          />
        )}
        {tab === 'invite' && (
          <InviteUserTab
            onInvited={refetchInvites}
            push={push}
            invite={invite}
            showLink={(email, link) => setModal({ kind: 'invitationLink', email, link })}
          />
        )}
        {tab === 'pending' && (
          <PendingTab
            invitations={invitations}
            usernameById={Object.fromEntries(users.map((u) => [u.id, u.username]))}
            push={push}
            resend={resend}
            cancel={cancel}
            refetch={refetchInvites}
            showLink={(email, link) => setModal({ kind: 'invitationLink', email, link })}
          />
        )}
      </div>

      {/* ---- Modals ---- */}
      {modal?.kind === 'confirmRole' && (
        <ConfirmModal
          title={modal.to === 'admin' ? 'Promote User' : 'Demote User'}
          body={
            modal.to === 'admin'
              ? `Make ${modal.user.username} an Admin? Admins can manage users and invitations.`
              : `Make ${modal.user.username} a regular user? They will lose access to user management.`
          }
          confirmLabel={modal.to === 'admin' ? 'Promote' : 'Demote'}
          danger={false}
          onCancel={() => setModal(null)}
          onConfirm={() => void doChangeRole(modal.user, modal.to)}
        />
      )}
      {modal?.kind === 'confirmDelete' && (
        <ConfirmModal
          title="Delete User"
          body={`Delete ${modal.user.username}? This cannot be undone, and all of this user's monitors will be deleted.`}
          confirmLabel="Delete"
          danger
          onCancel={() => setModal(null)}
          onConfirm={() => void doDelete(modal.user)}
        />
      )}
      {modal?.kind === 'resetPw' && (
        <ResetPasswordModal
          user={modal.user}
          push={push}
          resetPw={resetPw}
          resetPwAuto={resetPwAuto}
          onClose={() => setModal(null)}
          showPassword={(username, password) => setModal({ kind: 'password', title: 'Password reset', username, password })}
        />
      )}
      {modal?.kind === 'password' && (
        <PasswordModal title={modal.title} username={modal.username} password={modal.password} push={push} onDone={() => setModal(null)} />
      )}
      {modal?.kind === 'invitationLink' && (
        <LinkModal email={modal.email} link={modal.link} push={push} onDone={() => setModal(null)} />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}

// ---------------- Tabs ----------------

function CreateUserTab({
  onCreated,
  push,
  createUser,
  createUserAuto,
  showPassword,
}: {
  onCreated: () => void
  push: (m: string, t?: 'success' | 'error' | 'info') => void
  createUser: (u: string, e: string, p: string, r: Role) => Promise<ManagedUser>
  createUserAuto: (u: string, e: string, r: Role) => Promise<{ user: ManagedUser; temporary_password: string }>
  showPassword: (username: string, password: string) => void
}) {
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('user')
  const [mode, setMode] = useState<'manual' | 'auto'>('auto')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    if (!username.trim() || !email.trim()) {
      push('Username and email are required', 'error')
      return
    }
    setBusy(true)
    try {
      if (mode === 'auto') {
        const { user, temporary_password } = await createUserAuto(username.trim(), email.trim(), role)
        showPassword(user.username, temporary_password)
      } else {
        await createUser(username.trim(), email.trim(), password, role)
        push(`User ${username.trim()} created`, 'success')
      }
      setUsername('')
      setEmail('')
      setPassword('')
      setRole('user')
      onCreated()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to create user', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid max-w-xl gap-3">
      <input className={inputCls} placeholder="Username" value={username} onChange={(e) => setUsername(e.target.value)} />
      <input className={inputCls} type="email" placeholder="Email" value={email} onChange={(e) => setEmail(e.target.value)} />
      <select className={inputCls} value={role} onChange={(e) => setRole(e.target.value as Role)}>
        <option value="user">Role: User</option>
        <option value="admin">Role: Admin</option>
      </select>
      <div className="flex flex-wrap gap-4 text-sm">
        <label className="flex items-center gap-2">
          <input type="radio" checked={mode === 'auto'} onChange={() => setMode('auto')} /> Auto-generate password
        </label>
        <label className="flex items-center gap-2">
          <input type="radio" checked={mode === 'manual'} onChange={() => setMode('manual')} /> I&apos;ll set password
        </label>
      </div>
      {mode === 'manual' ? (
        <input className={inputCls} type="password" placeholder="Password (min 12 chars)" value={password} onChange={(e) => setPassword(e.target.value)} />
      ) : (
        <p className="text-xs text-neutral-400">A temporary password will be shown once after creation.</p>
      )}
      <button className="btn-primary w-fit" disabled={busy} onClick={() => void submit()}>
        {busy ? 'Creating…' : 'Create User'}
      </button>
    </div>
  )
}

function InviteUserTab({
  onInvited,
  push,
  invite,
  showLink,
}: {
  onInvited: () => void
  push: (m: string, t?: 'success' | 'error' | 'info') => void
  invite: (e: string, r: Role, s: boolean) => Promise<{ email: string; token: string; email_warning?: string; email_sent?: boolean }>
  showLink: (email: string, link: string) => void
}) {
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('user')
  const [delivery, setDelivery] = useState<'email' | 'manual'>('manual')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    if (!email.trim() || !email.includes('@')) {
      push('A valid email is required', 'error')
      return
    }
    setBusy(true)
    try {
      const inv = await invite(email.trim(), role, delivery === 'email')
      if (delivery === 'email') {
        if (inv.email_warning) push(`Invitation created, but email failed: ${inv.email_warning}`, 'error')
        else push(`Invitation email sent to ${inv.email}`, 'success')
      } else {
        push(`Invitation created for ${inv.email}`, 'success')
        showLink(inv.email, invitationLink(inv.token))
      }
      setEmail('')
      setRole('user')
      onInvited()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to create invitation', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid max-w-xl gap-3">
      <input className={inputCls} type="email" placeholder="Email to invite" value={email} onChange={(e) => setEmail(e.target.value)} />
      <select className={inputCls} value={role} onChange={(e) => setRole(e.target.value as Role)}>
        <option value="user">Role: User</option>
        <option value="admin">Role: Admin</option>
      </select>
      <div className="flex flex-wrap gap-4 text-sm">
        <label className="flex items-center gap-2">
          <input type="radio" checked={delivery === 'manual'} onChange={() => setDelivery('manual')} /> Manual link
        </label>
        <label className="flex items-center gap-2">
          <input type="radio" checked={delivery === 'email'} onChange={() => setDelivery('email')} /> Send email (needs SMTP)
        </label>
      </div>
      <p className="text-xs text-neutral-400">
        {delivery === 'email'
          ? 'An invitation email will be sent if SMTP is configured; otherwise you can copy the link.'
          : "You'll get a link to share manually. It expires in 7 days."}
      </p>
      <button className="btn-primary w-fit" disabled={busy} onClick={() => void submit()}>
        <Send className="h-4 w-4" /> {busy ? 'Sending…' : 'Send Invitation'}
      </button>
    </div>
  )
}

function PendingTab({
  invitations,
  usernameById,
  push,
  resend,
  cancel,
  refetch,
  showLink,
}: {
  invitations: import('@/hooks/useUserManagement').PendingInvitation[]
  usernameById: Record<string, string>
  push: (m: string, t?: 'success' | 'error' | 'info') => void
  resend: (id: string) => Promise<void>
  cancel: (id: string) => Promise<void>
  refetch: () => void
  showLink: (email: string, link: string) => void
}) {
  const [confirmCancel, setConfirmCancel] = useState<string | null>(null)

  if (invitations.length === 0) {
    return <p className="text-sm text-neutral-500 dark:text-neutral-400">No pending invitations.</p>
  }
  const doResend = async (id: string, email: string) => {
    try {
      await resend(id)
      push(`Invitation resent to ${email}`, 'success')
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to resend', 'error')
    }
  }
  const doCancel = async (id: string, email: string) => {
    try {
      await cancel(id)
      push(`Invitation to ${email} cancelled`, 'success')
      setConfirmCancel(null)
      refetch()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to cancel', 'error')
    }
  }

  return (
    <div className="space-y-2">
      {invitations.map((inv) => {
        const expiresSoon = new Date(inv.expires_at).getTime() - Date.now() < 24 * 3600e3
        return (
          <div key={inv.id} className="rounded-md border border-neutral-200 p-3 dark:border-neutral-800">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="flex items-center gap-2 font-medium">
                  {inv.email} <RoleBadge role={inv.role} />
                </div>
                <div className="text-xs text-neutral-400">
                  Invited by {usernameById[inv.invited_by_user_id] ?? 'admin'} ·{' '}
                  <span className={expiresSoon ? 'text-red-500' : ''}>
                    expires {format(new Date(inv.expires_at), 'MMM d, yyyy')}
                  </span>
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5">
                <button className="btn-secondary !py-1 !px-2" onClick={() => void doResend(inv.id, inv.email)}>
                  <Mail className="h-4 w-4" /> Resend
                </button>
                <button className="btn-secondary !py-1 !px-2" onClick={() => showLink(inv.email, invitationLink(inv.token))}>
                  <LinkIcon className="h-4 w-4" /> Copy Link
                </button>
                {confirmCancel === inv.id ? (
                  <>
                    <button className="btn bg-error-600 !py-1 !px-2 text-white" onClick={() => void doCancel(inv.id, inv.email)}>
                      Confirm
                    </button>
                    <button className="btn-secondary !py-1 !px-2" onClick={() => setConfirmCancel(null)}>
                      No
                    </button>
                  </>
                ) : (
                  <button className="btn-secondary !py-1 !px-2 text-error-600" onClick={() => setConfirmCancel(inv.id)}>
                    Cancel
                  </button>
                )}
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}

// ---------------- Modals ----------------

function Overlay({ children, onClose }: { children: React.ReactNode; onClose?: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>
  )
}

function ConfirmModal({
  title,
  body,
  confirmLabel,
  danger,
  onCancel,
  onConfirm,
}: {
  title: string
  body: string
  confirmLabel: string
  danger: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <Overlay onClose={onCancel}>
      <h3 className="text-lg font-semibold">{title}</h3>
      <p className="text-sm text-neutral-500 dark:text-neutral-400">{body}</p>
      <div className="flex justify-end gap-2">
        <button className="btn-secondary" onClick={onCancel}>
          Cancel
        </button>
        <button className={danger ? 'btn bg-error-600 text-white hover:bg-error-700' : 'btn-primary'} onClick={onConfirm}>
          {confirmLabel}
        </button>
      </div>
    </Overlay>
  )
}

function PasswordModal({
  title,
  username,
  password,
  push,
  onDone,
}: {
  title: string
  username: string
  password: string
  push: (m: string, t?: 'success' | 'error' | 'info') => void
  onDone: () => void
}) {
  return (
    <Overlay>
      <h3 className="text-lg font-semibold">{title}</h3>
      <p className="text-sm text-neutral-500 dark:text-neutral-400">
        Temporary password for <span className="font-medium">{username}</span>. It is shown only once — copy it now.
      </p>
      <div className="flex items-center gap-2">
        <code className="flex-1 break-all rounded-md bg-neutral-100 p-2 font-mono text-sm dark:bg-neutral-800">{password}</code>
        <button className="btn-secondary !px-2" onClick={() => void copy(password, push)} title="Copy">
          <Copy className="h-4 w-4" />
        </button>
      </div>
      <div className="flex justify-end">
        <button className="btn-primary" onClick={onDone}>
          Done
        </button>
      </div>
    </Overlay>
  )
}

function LinkModal({
  email,
  link,
  push,
  onDone,
}: {
  email: string
  link: string
  push: (m: string, t?: 'success' | 'error' | 'info') => void
  onDone: () => void
}) {
  return (
    <Overlay onClose={onDone}>
      <div className="flex items-start justify-between">
        <h3 className="text-lg font-semibold">Invitation Link</h3>
        <button className="text-neutral-400 hover:text-neutral-600" onClick={onDone}>
          <X className="h-5 w-5" />
        </button>
      </div>
      <p className="text-sm text-neutral-500 dark:text-neutral-400">Share this link with {email}:</p>
      <div className="flex items-center gap-2">
        <input readOnly className={`${inputCls} font-mono`} value={link} onFocus={(e) => e.target.select()} />
        <button className="btn-secondary !px-2" onClick={() => void copy(link, push)} title="Copy">
          <Copy className="h-4 w-4" />
        </button>
      </div>
      <div className="flex justify-end">
        <button className="btn-primary" onClick={onDone}>
          Done
        </button>
      </div>
    </Overlay>
  )
}

function ResetPasswordModal({
  user,
  push,
  resetPw,
  resetPwAuto,
  onClose,
  showPassword,
}: {
  user: ManagedUser
  push: (m: string, t?: 'success' | 'error' | 'info') => void
  resetPw: (id: string, pw: string) => Promise<void>
  resetPwAuto: (id: string) => Promise<string>
  onClose: () => void
  showPassword: (username: string, password: string) => void
}) {
  const [mode, setMode] = useState<'manual' | 'auto'>('auto')
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    try {
      if (mode === 'auto') {
        const temp = await resetPwAuto(user.id)
        showPassword(user.username, temp) // replaces this modal with the password modal
      } else {
        await resetPw(user.id, pw)
        push(`Password reset for ${user.username}`, 'success')
        onClose()
      }
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to reset password', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Overlay onClose={onClose}>
      <h3 className="text-lg font-semibold">Reset Password</h3>
      <p className="text-sm text-neutral-500 dark:text-neutral-400">
        Resetting the password for <span className="font-medium">{user.username}</span>.
      </p>
      <div className="flex flex-wrap gap-4 text-sm">
        <label className="flex items-center gap-2">
          <input type="radio" checked={mode === 'auto'} onChange={() => setMode('auto')} /> Auto-generate
        </label>
        <label className="flex items-center gap-2">
          <input type="radio" checked={mode === 'manual'} onChange={() => setMode('manual')} /> Set manually
        </label>
      </div>
      {mode === 'manual' && (
        <input className={inputCls} type="password" placeholder="New password (min 12 chars)" value={pw} onChange={(e) => setPw(e.target.value)} />
      )}
      <div className="flex justify-end gap-2">
        <button className="btn-secondary" onClick={onClose}>
          Cancel
        </button>
        <button className="btn-primary" disabled={busy} onClick={() => void submit()}>
          {busy ? 'Resetting…' : 'Reset'}
        </button>
      </div>
    </Overlay>
  )
}
