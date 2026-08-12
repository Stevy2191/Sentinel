import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useNotificationConfigs, CHANNEL_META, CHANNEL_ORDER, type ChannelName } from '@/hooks/useNotificationConfig'

/**
 * The three states a monitor's notify_channels can be in. They are distinct
 * values rather than a checkbox list because "everything" and "everything that
 * happens to be enabled today" are different intentions: `null` follows the
 * global channel set as it changes, while a subset is a deliberate pin.
 */
export type NotifyMode = 'all' | 'none' | 'some'

export function modeOf(channels: string[] | null | undefined): NotifyMode {
  if (channels == null) return 'all'
  return channels.length === 0 ? 'none' : 'some'
}

interface Props {
  /** null = all channels, [] = none, [...] = only those. */
  value: string[] | null
  onChange: (value: string[] | null) => void
}

/**
 * NotificationChannelPicker chooses where a monitor's alerts go. Shared by the
 * creation wizard and the monitor edit form so the choice can be revisited
 * later rather than being fixed at creation.
 */
export default function NotificationChannelPicker({ value, onChange }: Props) {
  const { configs, loading } = useNotificationConfigs()

  // The chosen mode has to be held separately from the value: picking "only
  // specific channels" before ticking any produces [], which is indistinguish-
  // able from "none". Deriving the mode from the value alone would snap the
  // radio back to "none" and hide the checkboxes the user just asked for.
  const [mode, setMode] = useState<NotifyMode>(modeOf(value))
  const selected = value ?? []

  const pick = (next: NotifyMode) => {
    setMode(next)
    if (next === 'all') onChange(null)
    else if (next === 'none') onChange([])
    else onChange(selected)
  }

  // Only channels that are actually configured and switched on can deliver, so
  // those are the only ones worth offering.
  const available = CHANNEL_ORDER.filter((c) =>
    configs.some((cfg) => cfg.channel === c && cfg.enabled)
  )

  const toggle = (channel: ChannelName) => {
    const next = selected.includes(channel)
      ? selected.filter((c) => c !== channel)
      : [...selected, channel]
    onChange(next)
  }

  const OptionRow = ({
    option,
    title,
    detail,
  }: {
    option: NotifyMode
    title: string
    detail: string
  }) => (
    <label
      className="flex cursor-pointer items-start gap-3 rounded-lg p-3 transition-colors"
      style={{
        border: `1px solid ${mode === option ? 'var(--vs-cyan)' : 'var(--vs-line)'}`,
        backgroundColor: mode === option ? 'rgba(61, 225, 255, 0.06)' : 'transparent',
      }}
    >
      <input
        type="radio"
        name="notify-mode"
        className="mt-1"
        checked={mode === option}
        onChange={() => pick(option)}
      />
      <span className="min-w-0">
        <span className="block text-sm font-medium" style={{ color: 'var(--vs-text)' }}>
          {title}
        </span>
        <span className="block text-xs" style={{ color: 'var(--vs-text-dim)' }}>
          {detail}
        </span>
      </span>
    </label>
  )

  return (
    <div className="space-y-2">
      <OptionRow
        option="all"
        title="All configured channels"
        detail={
          available.length > 0
            ? `Currently: ${available.map((c) => CHANNEL_META[c].label).join(', ')}. Follows any channel you add later.`
            : 'No channels are configured yet — alerts will start flowing once you set one up.'
        }
      />
      <OptionRow option="none" title="No notifications" detail="Track this monitor silently. Incidents are still recorded." />
      <OptionRow option="some" title="Only specific channels" detail="Pin this monitor to the channels you choose." />

      {mode === 'some' && (
        <div className="ml-6 space-y-1.5 pt-1">
          {loading ? (
            <p className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              Loading channels…
            </p>
          ) : available.length === 0 ? (
            <p className="text-xs" style={{ color: 'var(--vs-amber)' }}>
              No channels are configured yet.{' '}
              <Link to="/notifications" className="underline">
                Set one up
              </Link>{' '}
              first, or choose “No notifications”.
            </p>
          ) : (
            available.map((c) => (
              <label key={c} className="flex cursor-pointer items-center gap-2 text-sm">
                <input type="checkbox" checked={selected.includes(c)} onChange={() => toggle(c)} />
                <span>{CHANNEL_META[c].emoji}</span>
                <span style={{ color: 'var(--vs-text)' }}>{CHANNEL_META[c].label}</span>
              </label>
            ))
          )}
          {available.length > 0 && selected.length === 0 && (
            <p className="text-xs" style={{ color: 'var(--vs-amber)' }}>
              Nothing selected — this monitor would not alert anywhere.
            </p>
          )}
        </div>
      )}
    </div>
  )
}
