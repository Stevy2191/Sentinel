import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChevronDown, FolderPlus, Globe, Plus, Radar, Router, Server, Upload, Wand2 } from 'lucide-react'
import { useDismissOnOutsideClick } from '@/hooks/useDismissOnOutsideClick'

interface Props {
  isAdmin: boolean
  onMonitor: () => void
  onGroup: () => void
  onServer: () => void
  onDevice: () => void
}

/** The page's one "+ Add" menu: every create action of the old Uptime,
 *  Servers and Devices pages, the admin-only ones for admins only. */
export default function AddMenu({ isAdmin, onMonitor, onGroup, onServer, onDevice }: Props) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const ref = useDismissOnOutsideClick(open, useCallback(() => setOpen(false), []))

  const items = [
    { key: 'monitor', label: 'Uptime monitor', icon: Globe, onClick: onMonitor },
    { key: 'wizard', label: 'Monitor wizard', icon: Wand2, onClick: () => navigate('/monitors/new/wizard') },
    { key: 'bulk', label: 'Bulk upload', icon: Upload, onClick: () => navigate('/monitors/bulk') },
    { key: 'group', label: 'Monitor group', icon: FolderPlus, onClick: onGroup },
    ...(isAdmin
      ? [
          { key: 'discover', label: 'Network discovery', icon: Radar, onClick: () => navigate('/monitors/discover') },
          { key: 'server', label: 'Server agent', icon: Server, onClick: onServer },
          { key: 'device', label: 'Network device', icon: Router, onClick: onDevice },
        ]
      : []),
  ]

  return (
    <div ref={ref} className="relative shrink-0">
      <button className="rd-btn rd-btn-primary" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <Plus className="h-4 w-4" /> Add
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-40 mt-2 w-56 overflow-hidden rounded-lg border border-white/10 bg-slate-900 py-1 shadow-xl">
          {items.map((item) => (
            <button
              key={item.key}
              role="menuitem"
              onClick={() => {
                setOpen(false)
                item.onClick()
              }}
              className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-white transition-colors hover:bg-white/5"
            >
              <item.icon className="h-4 w-4 shrink-0 text-cyan-400" />
              <span className="flex-1">{item.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
