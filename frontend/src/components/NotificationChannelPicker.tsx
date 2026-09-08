import { useState } from 'react'
import { useAvailableChannels } from '@/hooks/useNotificationConfig'
import NotificationsSection from './NotificationsSection'

/**
 * The three states a monitor's notify_channels can be in. `null` follows the
 * global channel set as it changes, `[]` alerts nowhere, and a list is a
 * deliberate pin.
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
 * NotificationChannelPicker chooses where an existing monitor's alerts go.
 *
 * It presents the same switch and dropdown as the create dialogs — the choice
 * should not look like a different feature depending on whether the monitor is
 * being made or edited — while keeping the `null | [] | [...]` value it has
 * always exchanged with the forms around it.
 *
 * `null` is shown as on with every channel selected, which is what it means.
 * It is only rewritten to an explicit list if this control is actually used,
 * so opening a monitor to change its timeout does not quietly convert "every
 * channel, including ones added later" into a pin on today's set.
 */
export default function NotificationChannelPicker({ value, onChange }: Props) {
  const { available } = useAvailableChannels(true)

  // Held separately from the value because turning the switch on before
  // choosing anything produces [], which is indistinguishable from off.
  // Deriving it from the value alone would snap the switch back off and hide
  // the dropdown the user just asked for.
  const [enabled, setEnabled] = useState(modeOf(value) !== 'none')

  const selected = value ?? available.map((c) => c.id)

  return (
    <NotificationsSection
      showHeading={false}
      enabled={enabled}
      onEnabledChange={(v) => {
        setEnabled(v)
        // Turning it on pins the channels that exist now. There is no way to
        // express "and any added later" with a switch and a list, and silently
        // keeping that meaning behind a control that does not show it would be
        // worse than being explicit.
        onChange(v ? available.map((c) => c.id) : [])
      }}
      selected={selected}
      onSelectedChange={onChange}
      silentNote="Track this monitor silently. Incidents are still recorded."
    />
  )
}
