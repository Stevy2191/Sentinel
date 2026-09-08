import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { useAvailableChannels } from '@/hooks/useNotificationConfig'
import ChannelMultiSelect from './ChannelMultiSelect'

interface Props {
  enabled: boolean
  onEnabledChange: (v: boolean) => void
  selected: string[]
  onSelectedChange: (ids: string[]) => void
  /** Only fetch the channel list while the containing dialog is open. */
  active?: boolean
  /** What this service is, for the off-state wording. */
  silentNote: string
  /** Closes the dialog before the Settings link navigates away. */
  onNavigateAway?: () => void
}

/**
 * The Notifications section: a switch, and a channel dropdown once it is on.
 *
 * Shared by the monitor and domain dialogs rather than written twice, so the
 * two cannot drift apart in wording or behaviour.
 */
export default function NotificationsSection({
  enabled,
  onEnabledChange,
  selected,
  onSelectedChange,
  active = true,
  silentNote,
  onNavigateAway,
}: Props) {
  const { available, loading, error } = useAvailableChannels(active)
  const none = !loading && !error && available.length === 0

  return (
    <section>
      <h3 className="text-xs font-semibold uppercase tracking-widest text-slate-300">
        Notifications
      </h3>
      <p className="mb-4 mt-1 text-xs text-slate-500">
        Select which channels to notify for this service
      </p>

      <div className="flex items-center justify-between gap-4 rounded-lg border border-white/10 bg-slate-800/40 p-4">
        <div className="min-w-0">
          <p className="text-sm font-medium text-white">Enable Notifications</p>
          <p className="text-xs text-slate-500">
            {enabled ? 'Alert the channels you choose below.' : silentNote}
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label="Enable Notifications"
          onClick={() => onEnabledChange(!enabled)}
          className={`relative h-7 w-12 shrink-0 rounded-full transition-colors ${
            enabled ? 'bg-emerald-600' : 'bg-slate-600'
          }`}
        >
          <span
            className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white transition-transform ${
              enabled ? 'translate-x-5' : 'translate-x-0'
            }`}
          />
        </button>
      </div>

      {enabled && (
        <div className="mt-3">
          <span className="mb-1 block text-sm font-medium text-white">
            Select notification channels
          </span>

          {loading ? (
            <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-4 text-sm text-slate-400">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading channels…
            </div>
          ) : error ? (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">
              {error}
            </div>
          ) : (
            <>
              {/* The control stays visible but disabled when there is nothing
                  to choose, so the reason sits next to the thing it explains
                  rather than replacing it. */}
              <ChannelMultiSelect
                channels={available}
                selected={selected}
                onChange={onSelectedChange}
                disabled={none}
                label="Select notification channels"
              />
              {none ? (
                <p className="mt-1 text-xs text-amber-300">
                  No notification channels configured.{' '}
                  <Link
                    to="/settings"
                    onClick={onNavigateAway}
                    className="underline underline-offset-2"
                  >
                    Create one in Settings.
                  </Link>
                </p>
              ) : selected.length === 0 ? (
                <p className="mt-1 text-xs text-amber-300">
                  No channels selected yet — nothing will be alerted until you pick one.
                </p>
              ) : null}
            </>
          )}
        </div>
      )}
    </section>
  )
}
