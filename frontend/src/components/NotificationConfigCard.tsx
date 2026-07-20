import { Send, Settings, CheckCircle2, XCircle, MinusCircle } from 'lucide-react'
import {
  CHANNEL_META,
  type ChannelName,
  type NotificationConfig,
} from '@/hooks/useNotificationConfig'

interface Props {
  channel: ChannelName
  config?: NotificationConfig // undefined = never configured
  testing?: boolean
  onConfigure: () => void
  onTest: () => void
}

// LastTestIndicator renders a small colored line reflecting the last test.
function LastTestIndicator({ config }: { config?: NotificationConfig }) {
  if (!config || config.last_test_success == null) {
    return (
      <div className="flex items-center gap-1.5 text-sm text-neutral-400">
        <MinusCircle className="h-4 w-4" /> Never tested
      </div>
    )
  }
  if (config.last_test_success) {
    return (
      <div className="flex items-center gap-1.5 text-sm text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="h-4 w-4" /> Last test passed
      </div>
    )
  }
  return (
    <div className="flex items-center gap-1.5 text-sm text-red-500" title={config.last_test_error ?? ''}>
      <XCircle className="h-4 w-4" /> Last test failed
    </div>
  )
}

export default function NotificationConfigCard({ channel, config, testing, onConfigure, onTest }: Props) {
  const meta = CHANNEL_META[channel]
  const enabled = config?.enabled ?? false

  return (
    <div className="card flex flex-col p-5 transition duration-150 hover:scale-[1.02] hover:shadow-card-hover">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2">
          <span className="text-2xl" aria-hidden>
            {meta.emoji}
          </span>
          <span className="text-lg font-semibold">{meta.label}</span>
        </div>
        <span
          className={`rounded-md px-2 py-1 text-xs font-medium ${
            enabled
              ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
              : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
          }`}
        >
          {enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      <p className="mt-1 text-sm text-neutral-500 dark:text-neutral-400">{meta.description}</p>

      <div className="mt-3">
        <LastTestIndicator config={config} />
      </div>

      <div className="mt-4 flex gap-2 pt-1">
        <button
          className="btn-primary !py-1.5"
          disabled={!enabled || testing}
          onClick={onTest}
          title={enabled ? 'Send a test notification' : 'Configure and enable this channel first'}
        >
          <Send className={`h-4 w-4 ${testing ? 'animate-pulse' : ''}`} />
          {testing ? 'Testing…' : 'Test'}
        </button>
        <button className="btn-secondary !py-1.5" onClick={onConfigure}>
          <Settings className="h-4 w-4" /> Configure
        </button>
      </div>
    </div>
  )
}
