import { Send, Settings, CheckCircle2, XCircle } from 'lucide-react'
import type { NotificationChannel } from '@/types'

interface Props {
  channel: NotificationChannel
  testing: boolean
  onTest: (name: string) => void
  onConfigure: () => void
}

export default function NotificationChannelCard({ channel, testing, onTest, onConfigure }: Props) {
  return (
    <div className="card p-5 transition duration-150 hover:scale-[1.02] hover:shadow-card-hover">
      <div className="flex items-start justify-between">
        <div className="text-lg font-semibold capitalize">{channel.name}</div>
        <span
          className={`rounded-md px-2 py-1 text-xs font-medium ${
            channel.enabled
              ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
              : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
          }`}
        >
          {channel.enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      <p className="mt-1 text-sm text-neutral-500 dark:text-neutral-400">
        {channel.description || `Send alerts via ${channel.name}`}
      </p>

      <div
        className={`mt-3 flex items-center gap-1.5 text-sm font-medium ${
          channel.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-500'
        }`}
      >
        {channel.enabled ? (
          <>
            <CheckCircle2 className="h-4 w-4" /> Configured
          </>
        ) : (
          <>
            <XCircle className="h-4 w-4" /> Not Configured
          </>
        )}
      </div>

      <div className="mt-4 flex gap-2">
        <button
          className="btn-primary !py-1.5"
          disabled={!channel.enabled || testing}
          onClick={() => onTest(channel.name)}
        >
          <Send className={`h-4 w-4 ${testing ? 'animate-pulse' : ''}`} />
          {testing ? 'Sending…' : 'Test'}
        </button>
        <button className="btn-secondary !py-1.5" onClick={onConfigure}>
          <Settings className="h-4 w-4" /> Configure
        </button>
      </div>
    </div>
  )
}
