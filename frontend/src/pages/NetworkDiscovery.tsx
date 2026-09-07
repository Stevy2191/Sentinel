import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, Loader2, Radar, Signal, X } from 'lucide-react'
import api from '@/services/api'
import { useScanSubnet } from '@/hooks/useDiscovery'
import { useToasts, Toaster } from '@/components/Toast'
import { useAuthContext } from '@/context/AuthContext'
import type { ApiResponse, DiscoveredHost, MonitorInput } from '@/types'

const DEFAULT_INTERVAL_SECONDS = 60
const DEFAULT_TIMEOUT_SECONDS = 10

/** Same shape /monitors/bulk returns, reused from the BulkUpload flow. */
interface BulkResponse {
  dry_run: boolean
  total: number
  valid: number
  invalid: number
  created: number
  results: { index: number; name: string; valid: boolean; created: boolean; id?: string; error?: string }[]
}

/** hostLabel picks the friendliest available name for a discovered host. */
function hostLabel(host: DiscoveredHost): string {
  return host.hostname?.trim() || host.ip
}

function toMonitorInput(host: DiscoveredHost): MonitorInput {
  return {
    name: hostLabel(host),
    type: 'ping',
    url: host.ip,
    interval_seconds: DEFAULT_INTERVAL_SECONDS,
    timeout_seconds: DEFAULT_TIMEOUT_SECONDS,
    tags: ['Discovered'],
  }
}

/**
 * NetworkDiscovery — sweep a subnet (e.g. 192.168.1.0/24) for live hosts and
 * add a selection of them as Ping Monitors in one step. The scan itself is a
 * no-agent host discovery (ICMP with a TCP-port fallback) run server-side by
 * DiscoveryService; this page is just the picker on top of it, using the same
 * /monitors/bulk endpoint BulkUpload uses so creation behaves identically.
 */
export default function NetworkDiscovery() {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const isAdmin = currentUser?.is_admin ?? false
  const { toasts, push } = useToasts()
  const { scan, result, loading: scanning, error: scanError } = useScanSubnet()

  useEffect(() => {
    if (currentUser && !isAdmin) navigate('/dashboard')
  }, [currentUser, isAdmin, navigate])

  const [cidr, setCidr] = useState('192.168.1.0/24')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [adding, setAdding] = useState(false)
  const [done, setDone] = useState<BulkResponse | null>(null)

  // Memoised so the fallback [] keeps a stable identity: a fresh array each
  // render would invalidate every downstream useMemo that depends on it.
  const hosts = useMemo(() => result?.hosts ?? [], [result])
  const allSelected = hosts.length > 0 && selected.size === hosts.length

  const runScan = async () => {
    setDone(null)
    setSelected(new Set())
    try {
      const scanned = await scan(cidr.trim())
      // Nothing found isn't an error, but it's worth calling out explicitly.
      if (scanned.hosts.length === 0) {
        push(`No hosts responded on ${scanned.cidr}`, 'info')
      }
    } catch (err) {
      push((err as { message?: string }).message ?? 'Scan failed', 'error')
    }
  }

  const toggleHost = (ip: string) =>
    setSelected((cur) => {
      const next = new Set(cur)
      if (next.has(ip)) next.delete(ip)
      else next.add(ip)
      return next
    })

  const toggleAll = () =>
    setSelected((cur) => (cur.size === hosts.length ? new Set() : new Set(hosts.map((h) => h.ip))))

  const selectedHosts = useMemo(() => hosts.filter((h) => selected.has(h.ip)), [hosts, selected])

  const addSelected = async () => {
    if (selectedHosts.length === 0) return
    setAdding(true)
    try {
      const { data } = await api.post<ApiResponse<BulkResponse>>('/monitors/bulk', {
        monitors: selectedHosts.map(toMonitorInput),
        dry_run: false,
      })
      setDone(data.data)
      push(
        `Added ${data.data.created} of ${data.data.total} hosts as Ping Monitors`,
        data.data.created > 0 ? 'success' : 'error'
      )
    } catch (err) {
      push((err as { message?: string }).message ?? 'Could not add monitors', 'error')
    } finally {
      setAdding(false)
    }
  }

  const reset = () => {
    setDone(null)
    setSelected(new Set())
  }

  if (!currentUser || !isAdmin) return null

  // ---- after monitors have been added ----
  if (done) {
    const failures = done.results.filter((r) => !r.created)
    return (
      <div className="mx-auto max-w-3xl space-y-5 pb-10">
        <h1 className="vs-title text-2xl">Hosts added</h1>
        <div
          className="rd-card p-6"
          style={{ ['--rd-accent' as string]: done.created > 0 ? 'var(--vs-ecg)' : 'var(--vs-flat)' }}
        >
          <p className="vs-readout text-3xl" style={{ color: done.created > 0 ? 'var(--vs-ecg)' : 'var(--vs-flat)' }}>
            {done.created}
            <span className="text-base" style={{ color: 'var(--vs-text-dim)' }}> of {done.total} added as Ping Monitors</span>
          </p>
          {failures.length > 0 && (
            <div className="mt-4">
              <p className="vs-eyebrow mb-2">Not added</p>
              <ul className="space-y-1 text-sm">
                {failures.map((r) => (
                  <li key={r.index} style={{ color: 'var(--vs-text-dim)' }}>
                    {r.name || '(unnamed)'} — {r.error}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <button className="rd-btn rd-btn-primary" onClick={() => navigate('/dashboard')}>
            Go to dashboard
          </button>
          <button className="rd-btn rd-btn-secondary" onClick={reset}>
            Scan again
          </button>
        </div>
        <Toaster toasts={toasts} />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-4xl space-y-5 pb-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="vs-title text-2xl">Network discovery</h1>
        <button className="rd-btn rd-btn-secondary" onClick={() => navigate('/dashboard')}>
          <X className="h-4 w-4" /> Cancel
        </button>
      </div>

      <div className="rd-card p-5" style={{ ['--rd-accent' as string]: 'var(--vs-cyan)' }}>
        <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
          Scan a subnet for hosts that respond to a ping, then pick which ones to add as{' '}
          <strong style={{ color: 'var(--vs-text)' }}>Ping Monitors</strong>. Good for a first pass over a
          home lab or a new network segment instead of entering every IP by hand.
        </p>
        <p className="mt-2 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
          Up to a /22 (1024 addresses) in one scan; larger ranges should be split up.
        </p>
      </div>

      <div className="rd-card p-4" style={{ ['--rd-accent' as string]: 'var(--vs-cyan)' }}>
        <div className="flex flex-wrap items-center gap-2">
          <input
            value={cidr}
            onChange={(e) => setCidr(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && !scanning && cidr.trim() && void runScan()}
            placeholder="192.168.1.0/24"
            spellCheck={false}
            className="rd-input flex-1 py-2"
            aria-label="Subnet (CIDR)"
          />
          <button
            className="rd-btn rd-btn-primary"
            disabled={scanning || !cidr.trim()}
            onClick={() => void runScan()}
          >
            {scanning ? <Loader2 className="h-4 w-4 animate-spin" /> : <Radar className="h-4 w-4" />}
            {scanning ? 'Scanning…' : 'Scan subnet'}
          </button>
        </div>
      </div>

      {scanError && (
        <div className="rd-card p-4 text-sm" style={{ ['--rd-accent' as string]: 'var(--vs-flat)', color: 'var(--vs-flat)' }}>
          {scanError.message}
        </div>
      )}

      {result && (
        <div className="rd-card p-5" style={{ ['--rd-accent' as string]: hosts.length > 0 ? 'var(--vs-ecg)' : 'var(--vs-amber)' }}>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <p className="text-sm" style={{ color: 'var(--vs-text)' }}>
              <span style={{ color: 'var(--vs-ecg)' }}>{result.hosts_up}</span> of {result.hosts_scanned}{' '}
              addresses on <span className="vs-readout">{result.cidr}</span> responded
              <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                {' '}
                ({(result.duration_ms / 1000).toFixed(1)}s)
              </span>
            </p>
            {hosts.length > 0 && (
              <button className="rd-btn rd-btn-secondary" onClick={toggleAll}>
                {allSelected ? 'Deselect all' : 'Select all'}
              </button>
            )}
          </div>

          {hosts.length === 0 ? (
            <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
              No hosts answered. If this network blocks ICMP, try again — the scan also falls back to a
              handful of common TCP ports.
            </p>
          ) : (
            <div className="max-h-96 overflow-y-auto">
              <table className="w-full text-left text-sm">
                <thead className="sticky top-0" style={{ backgroundColor: 'var(--vs-panel-2)' }}>
                  <tr>
                    {['', 'IP', 'Hostname', 'Method', 'Response'].map((h) => (
                      <th key={h} className="vs-eyebrow px-2 py-1.5">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {hosts.map((host) => {
                    const checked = selected.has(host.ip)
                    return (
                      <tr
                        key={host.ip}
                        style={{ borderTop: '1px solid var(--vs-line)', cursor: 'pointer' }}
                        onClick={() => toggleHost(host.ip)}
                      >
                        <td className="px-2 py-1.5">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={() => toggleHost(host.ip)}
                            onClick={(e) => e.stopPropagation()}
                            aria-label={`Select ${host.ip}`}
                          />
                        </td>
                        <td className="vs-readout px-2 py-1.5" style={{ color: 'var(--vs-text)' }}>
                          {host.ip}
                        </td>
                        <td className="px-2 py-1.5" style={{ color: 'var(--vs-text-dim)' }}>
                          {host.hostname || <span>—</span>}
                        </td>
                        <td className="px-2 py-1.5 text-xs uppercase" style={{ color: 'var(--vs-text-dim)' }}>
                          <span className="inline-flex items-center gap-1">
                            <Signal className="h-3 w-3" /> {host.method}
                          </span>
                        </td>
                        <td className="vs-readout px-2 py-1.5 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                          {host.response_time_ms} ms
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}

          {hosts.length > 0 && (
            <div className="mt-4 flex flex-wrap items-center gap-2">
              <button
                className="rd-btn rd-btn-primary"
                disabled={selectedHosts.length === 0 || adding}
                onClick={() => void addSelected()}
              >
                {adding ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                Add {selectedHosts.length || ''} as Ping Monitor{selectedHosts.length === 1 ? '' : 's'}
              </button>
              <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                Each is created with a {DEFAULT_INTERVAL_SECONDS}s interval and a {DEFAULT_TIMEOUT_SECONDS}s
                timeout — adjust individually afterward if needed.
              </span>
            </div>
          )}
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
