import type { RefObject } from 'react'
import type { AgentOS } from '@/hooks/useAgents'

/** The settings an operator owns. Credentials are not among them. */
export interface AgentSettings {
  name: string
  osType: AgentOS
  ipOverride: string
  interval: number
  retries: number
}

export interface AgentSettingsErrors {
  name?: string
  ip?: string
  interval?: string
  retries?: string
}

const OS_OPTIONS: { value: AgentOS; label: string }[] = [
  { value: 'ubuntu', label: 'Ubuntu' },
  { value: 'debian', label: 'Debian' },
  { value: 'centos', label: 'CentOS' },
  { value: 'rhel', label: 'RHEL' },
  { value: 'windows', label: 'Windows' },
  { value: 'linux', label: 'Linux (Generic)' },
]

const field =
  'w-full rounded-lg border border-white/10 bg-slate-800/50 px-4 py-2 text-white placeholder-slate-500 transition focus:border-white/30 focus:outline-none'

/**
 * Whether a string is an IP address, matching what the server accepts.
 *
 * Both families, because an agent on an IPv6-only network has no IPv4 address
 * to give. Loose on the IPv6 side deliberately: the server is the authority,
 * and rejecting a valid address here would block a legitimate value.
 */
export function isIPAddress(value: string): boolean {
  const v4 = value.split('.')
  if (v4.length === 4) {
    return v4.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
  }
  return value.includes(':') && /^[0-9a-fA-F:.]+$/.test(value)
}

/**
 * Validates a settings form the way the server does, so a dialog does not
 * offer to save something that will be refused.
 *
 * `touched` gates only the name: an empty name is the starting state of a new
 * agent, and complaining about it before anything has been typed is noise. The
 * others start valid, so a complaint about them always follows an edit.
 */
export function validateAgentSettings(
  values: AgentSettings,
  touched: boolean,
): { errors: AgentSettingsErrors; valid: boolean } {
  const errors: AgentSettingsErrors = {}
  if (touched && !values.name.trim()) errors.name = 'A server name is required'
  if (values.ipOverride.trim() !== '' && !isIPAddress(values.ipOverride.trim())) {
    errors.ip = 'Must be an IP address, e.g. 192.168.1.50'
  }
  if (values.interval < 1 || values.interval > 3600) {
    errors.interval = 'Must be between 1 and 3600 seconds'
  }
  if (values.retries < 1 || values.retries > 10) errors.retries = 'Must be between 1 and 10'

  // Validity ignores the name's touched gate: a blank name is invalid whether
  // or not anyone has typed into the field yet.
  const valid =
    values.name.trim() !== '' && !errors.ip && !errors.interval && !errors.retries
  return { errors, valid }
}

/**
 * The agent settings form, shared by the create dialog and the edit dialog.
 *
 * One definition rather than two, so the two cannot disagree about what a
 * field means or which values it accepts.
 */
export default function AgentSettingsFields({
  values,
  onChange,
  errors,
  firstFieldRef,
}: {
  values: AgentSettings
  onChange: (next: AgentSettings) => void
  errors: AgentSettingsErrors
  firstFieldRef?: RefObject<HTMLInputElement>
}) {
  const set = <K extends keyof AgentSettings>(key: K, value: AgentSettings[K]) =>
    onChange({ ...values, [key]: value })

  return (
    <>
      <section>
        <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
          Server
        </h3>
        <div className="space-y-4">
          <div>
            <label htmlFor="agent-name" className="mb-1 block text-sm font-medium text-white">
              Server Name
            </label>
            <input
              id="agent-name"
              ref={firstFieldRef}
              value={values.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder="e.g. web-server-01"
              className={`${field} ${errors.name ? 'border-red-500/60' : ''}`}
            />
            <p className={`mt-1 text-xs ${errors.name ? 'text-red-400' : 'text-slate-500'}`}>
              {errors.name ?? 'How this host appears in Sentinel'}
            </p>
          </div>

          <div>
            <label htmlFor="agent-ip" className="mb-1 block text-sm font-medium text-white">
              IP Address
            </label>
            <input
              id="agent-ip"
              value={values.ipOverride}
              onChange={(e) => set('ipOverride', e.target.value)}
              placeholder="Auto-detect (optional)"
              className={`${field} ${errors.ip ? 'border-red-500/60' : ''}`}
            />
            <p className={`mt-1 text-xs ${errors.ip ? 'text-red-400' : 'text-slate-500'}`}>
              {errors.ip ??
                'Leave blank to auto-detect, or enter the internal address this host is reached on (e.g. 192.168.1.10).'}
            </p>
          </div>

          <div>
            <label htmlFor="agent-os" className="mb-1 block text-sm font-medium text-white">
              Operating System
            </label>
            <select
              id="agent-os"
              value={values.osType}
              onChange={(e) => set('osType', e.target.value as AgentOS)}
              className={`${field} cursor-pointer appearance-none`}
            >
              {OS_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-slate-500">
              Chooses which install commands are shown. The agent itself is the same build.
            </p>
          </div>
        </div>
      </section>

      <div className="border-t border-white/10" />

      <section>
        <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
          Collection
        </h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <label htmlFor="agent-interval" className="mb-1 block text-sm font-medium text-white">
              Check Interval
            </label>
            <div className="flex items-center gap-2">
              <input
                id="agent-interval"
                type="number"
                min={1}
                max={3600}
                value={values.interval}
                onChange={(e) => set('interval', Number(e.target.value))}
                className={`w-32 ${field} ${errors.interval ? 'border-red-500/60' : ''}`}
              />
              <span className="text-sm text-slate-400">seconds</span>
            </div>
            <p className={`mt-1 text-xs ${errors.interval ? 'text-red-400' : 'text-slate-500'}`}>
              {errors.interval ?? 'How often metrics are collected'}
            </p>
          </div>

          <div>
            <label htmlFor="agent-retries" className="mb-1 block text-sm font-medium text-white">
              Retry Attempts
            </label>
            <input
              id="agent-retries"
              type="number"
              min={1}
              max={10}
              value={values.retries}
              onChange={(e) => set('retries', Number(e.target.value))}
              className={`w-32 ${field} ${errors.retries ? 'border-red-500/60' : ''}`}
            />
            <p className={`mt-1 text-xs ${errors.retries ? 'text-red-400' : 'text-slate-500'}`}>
              {errors.retries ?? 'Before a submission is queued for later'}
            </p>
          </div>
        </div>
      </section>
    </>
  )
}
