import { useId, useMemo } from 'react'
import { useAgents } from '@/hooks/useAgents'
import { useDevices } from '@/hooks/useDevices'
import { useMonitors } from '@/hooks/useMonitors'
import type { NetTool } from '@/types/netTools'
import { knownHosts } from '@/utils/netTools'

interface Props {
  tool: NetTool
  value: string
  onChange: (v: string) => void
}

const MONITOR_FILTER = { limit: 500 }

/** The target, with the devices, monitored hosts and servers this user can
 *  see offered as suggestions. A suggestion is only a convenience: the
 *  allowlist decides, on the server. */
export default function TargetInput({ tool, value, onChange }: Props) {
  const id = useId()
  const listId = `${id}-hosts`
  const { devices } = useDevices()
  const { monitors } = useMonitors(MONITOR_FILTER)
  const { agents } = useAgents(0)
  const hosts = useMemo(() => knownHosts(devices, monitors, agents), [devices, monitors, agents])
  const dns = tool === 'dns'

  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-sm font-medium text-white">
        {dns ? 'Name to look up' : 'Target'}
      </label>
      <input
        id={id}
        list={listId}
        className="w-full font-mono"
        value={value}
        maxLength={253}
        autoComplete="off"
        spellCheck={false}
        placeholder={dns ? 'example.org, or an IPv4 address for PTR' : '10.0.0.1 or fileserver.example.org'}
        onChange={(e) => onChange(e.target.value)}
      />
      <datalist id={listId}>
        {hosts.map((h) => (
          <option key={h.value} value={h.value} label={h.label} />
        ))}
      </datalist>
      <p className="mt-1 text-xs text-slate-500">
        {dns
          ? 'The name is looked up as typed; only a named DNS server must be on the allowlist.'
          : 'Pick a known device, monitor or server, or type an address. The allowlist still applies.'}
      </p>
    </div>
  )
}
