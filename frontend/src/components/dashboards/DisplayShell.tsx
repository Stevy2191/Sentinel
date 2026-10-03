import { useEffect, useState, type ReactNode } from 'react'
import { Maximize2, X } from 'lucide-react'
import { KIOSK_HINT } from '@/utils/dashboards'

interface Props {
  title: string
  /** When the newest widget data arrived (epoch ms). */
  lastUpdated: number | null
  fullscreen: boolean
  onFullscreen: () => void
  onExit?: () => void
  children: ReactNode
}

function clock(t: number): string {
  return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

/** The wall-display frame: covers the app's chrome, shows the dashboard's
 *  name, the time and when data last arrived. */
export default function DisplayShell({ title, lastUpdated, fullscreen, onFullscreen, onExit, children }: Props) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 30000)
    return () => window.clearInterval(t)
  }, [])
  return (
    <div className="fixed inset-0 z-50 overflow-y-auto bg-slate-950 p-4">
      <header className="mb-4 flex items-center gap-4">
        <h1 className="min-w-0 flex-1 truncate text-3xl font-light text-white">{title}</h1>
        <span className="tabular-nums text-slate-400">
          {lastUpdated ? `Updated ${clock(lastUpdated)}` : 'Loading…'} · {clock(now)}
        </span>
        {!fullscreen && (
          <button className="btn-secondary flex items-center gap-2" onClick={onFullscreen} title={KIOSK_HINT}>
            <Maximize2 className="h-4 w-4" /> Fullscreen
          </button>
        )}
        {onExit && (
          <button className="btn-secondary flex items-center gap-2" onClick={onExit} aria-label="Leave the wall display">
            <X className="h-4 w-4" /> Exit
          </button>
        )}
      </header>
      {children}
    </div>
  )
}
