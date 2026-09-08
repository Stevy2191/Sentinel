import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { useAvailableChannels, CHANNEL_META } from '@/hooks/useNotificationConfig'

interface Props {
  /** Ids of the currently ticked channels. */
  selected: string[]
  onToggle: (id: string) => void
  /** Only fetch while the containing dialog is open. */
  enabled?: boolean
  /** Sits under the heading; says what these channels are notified about. */
  description: string
  /**
   * Shown when nothing is ticked, and again when no channel exists at all.
   * Each caller says what its own subject still does without alerts, since
   * "records incidents anyway" is true of a monitor but not of a domain.
   */
  emptyNote: string
  /** Closes the dialog before the Settings link navigates away. */
  onNavigateAway?: () => void
}

/**
 * ChannelChecklist is the flat tick-list of channels used by the modal
 * dialogs — the monitor creation modal and both domain dialogs.
 *
 * Distinct from NotificationChannelPicker, which offers the three-way
 * all/none/some choice the monitor edit form needs. A modal seeds every
 * channel ticked and sends the resulting list, so the extra mode would be a
 * control with nothing to do.
 *
 * Only configured, enabled channels are listed: offering a type nobody has set
 * up produces a selection that silently delivers nothing.
 */
export default function ChannelChecklist({
  selected,
  onToggle,
  enabled = true,
  description,
  emptyNote,
  onNavigateAway,
}: Props) {
  const { available, loading, error } = useAvailableChannels(enabled)

  return (
    <section>
      <h3 className="text-xs font-semibold uppercase tracking-widest text-slate-300">
        Notifications
      </h3>
      <p className="mb-4 mt-1 text-xs text-slate-500">{description}</p>

      {loading ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-4 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading channels…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">
          {error}
        </div>
      ) : available.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4 text-sm text-slate-400">
          <p>
            No notification channels configured.{' '}
            <Link
              to="/settings"
              onClick={onNavigateAway}
              className="text-emerald-400 underline-offset-2 hover:underline"
            >
              Create one in Settings.
            </Link>
          </p>
          <p className="mt-1 text-xs text-slate-500">{emptyNote}</p>
        </div>
      ) : (
        <div className="space-y-2">
          {/* The API returns them grouped by type in a stable order, so the
              list does not reshuffle between opens. */}
          {available.map((c) => (
            <label
              key={c.id}
              className="flex cursor-pointer items-center gap-3 rounded-lg border border-white/10 bg-slate-800/40 p-3 transition hover:border-white/20"
            >
              <input
                type="checkbox"
                checked={selected.includes(c.id)}
                onChange={() => onToggle(c.id)}
                className="h-4 w-4 shrink-0 rounded accent-emerald-500"
              />
              <span className="min-w-0">
                <span className="block truncate text-sm font-medium text-white">{c.name}</span>
                <span className="block text-xs text-slate-500">{CHANNEL_META[c.channel].label}</span>
              </span>
            </label>
          ))}
          {selected.length === 0 && <p className="pt-1 text-xs text-amber-300">{emptyNote}</p>}
        </div>
      )}
    </section>
  )
}

/**
 * Toggles one id in a selection, for the callers that keep the list in state.
 */
export function toggleChannelID(current: string[], id: string): string[] {
  return current.includes(id) ? current.filter((c) => c !== id) : [...current, id]
}
