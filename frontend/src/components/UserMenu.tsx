import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Shield, Settings as SettingsIcon, LogOut, ChevronUp } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'

export default function UserMenu() {
  const { currentUser, logout } = useAuthContext()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [open])

  const username = currentUser?.username ?? 'User'
  const initial = username.charAt(0).toUpperCase()

  const go = (path: string) => {
    setOpen(false)
    navigate(path)
  }
  const handleLogout = () => {
    setOpen(false)
    logout()
    navigate('/login')
  }

  return (
    <div ref={ref} className="relative">
      {open && (
        <div className="absolute bottom-full left-0 mb-2 w-full overflow-hidden rounded-md border border-neutral-200 bg-white shadow-card dark:border-neutral-700 dark:bg-neutral-800">
          <button
            onClick={() => go('/settings/security')}
            className="flex w-full items-center gap-2 px-3 py-2 text-sm hover:bg-neutral-100 dark:hover:bg-neutral-700"
          >
            <Shield className="h-4 w-4" /> Security
          </button>
          <button
            onClick={() => go('/settings')}
            className="flex w-full items-center gap-2 px-3 py-2 text-sm hover:bg-neutral-100 dark:hover:bg-neutral-700"
          >
            <SettingsIcon className="h-4 w-4" /> Settings
          </button>
          <button
            onClick={handleLogout}
            className="flex w-full items-center gap-2 border-t border-neutral-100 px-3 py-2 text-sm text-error-600 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-700"
          >
            <LogOut className="h-4 w-4" /> Log out
          </button>
        </div>
      )}

      <button
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-3 rounded-md p-2 text-left transition-colors hover:bg-neutral-100 dark:hover:bg-neutral-800"
      >
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-primary-600 text-sm font-semibold text-white">
          {initial}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold">{username}</span>
          <span className="block truncate text-xs text-neutral-500 dark:text-neutral-400">
            {currentUser?.is_admin ? 'Administrator' : 'Member'}
          </span>
        </span>
        <ChevronUp className={`h-4 w-4 text-neutral-400 transition-transform ${open ? '' : 'rotate-180'}`} />
      </button>
    </div>
  )
}
