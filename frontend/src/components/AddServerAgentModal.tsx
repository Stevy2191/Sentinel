import { useEffect, useRef, useState } from 'react'
import { X, Copy, Check, Loader2, ServerCog } from 'lucide-react'
import {
  useAgentActions,
  type AgentOS,
  type CreatedAgent,
} from '@/hooks/useAgents'
import type { ApiError } from '@/services/api'

interface Props {
  isOpen: boolean
  onClose: () => void
  onCreated: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
  /** Set to show install instructions for an agent that already exists. */
  existing?: CreatedAgent | null
}

const OS_OPTIONS: { value: AgentOS; label: string }[] = [
  { value: 'ubuntu', label: 'Ubuntu' },
  { value: 'debian', label: 'Debian' },
  { value: 'centos', label: 'CentOS' },
  { value: 'rhel', label: 'RHEL' },
  { value: 'windows', label: 'Windows' },
  { value: 'linux', label: 'Linux (Generic)' },
]

const TABS = ['One-Click Install', 'Docker One-Click', 'Direct Docker Run', 'Manual'] as const
type Tab = (typeof TABS)[number]

const field =
  'w-full rounded-lg border border-white/10 bg-slate-800/50 px-4 py-2 text-white placeholder-slate-500 transition focus:border-white/30 focus:outline-none'

/** A command block with its own copy button. */
function Command({ label, value }: { label?: string; value: string }) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      // Clipboard access is refused outside a secure context, which is common
      // on a LAN install reached over plain http. Falling back keeps the
      // button working rather than failing silently.
      const ta = document.createElement('textarea')
      ta.value = value
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      try {
        document.execCommand('copy')
      } finally {
        document.body.removeChild(ta)
      }
    }
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div>
      {label && <p className="mb-1 text-xs font-medium text-slate-400">{label}</p>}
      <div className="group relative">
        <pre className="overflow-x-auto rounded-lg border border-white/10 bg-slate-950/70 p-3 pr-12 text-xs leading-relaxed text-slate-200">
          {value}
        </pre>
        <button
          type="button"
          onClick={() => void copy()}
          aria-label={copied ? 'Copied' : 'Copy to clipboard'}
          className="absolute right-2 top-2 rounded-md border border-white/10 bg-slate-800/80 p-1.5 text-slate-300 transition hover:border-white/25 hover:text-white"
        >
          {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
    </div>
  )
}

/**
 * Registers a server agent and shows how to install it.
 *
 * Two steps rather than one: the install commands cannot exist before the
 * agent does, because they carry its id and token.
 */
export default function AddServerAgentModal({ isOpen, onClose, onCreated, push, existing }: Props) {
  const { create, busy } = useAgentActions()
  const firstFieldRef = useRef<HTMLInputElement>(null)

  const [name, setName] = useState('')
  const [osType, setOSType] = useState<AgentOS>('ubuntu')
  const [interval, setInterval] = useState(60)
  const [retries, setRetries] = useState(3)
  const [touched, setTouched] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Seeded from the prop rather than only in the effect below, so opening
  // this for an existing agent paints the instructions immediately instead of
  // showing the configuration form for a frame first.
  const [created, setCreated] = useState<CreatedAgent | null>(existing ?? null)
  const [tab, setTab] = useState<Tab>('One-Click Install')

  useEffect(() => {
    if (!isOpen) return
    setName('')
    setOSType('ubuntu')
    setInterval(60)
    setRetries(3)
    setTouched(false)
    setError(null)
    setTab('One-Click Install')
    // When opened for an agent that already exists, skip straight to the
    // instructions: nothing needs configuring.
    setCreated(existing ?? null)
    const t = window.setTimeout(() => firstFieldRef.current?.focus(), 50)
    return () => window.clearTimeout(t)
  }, [isOpen, existing])

  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [isOpen, busy, onClose])

  if (!isOpen) return null

  const nameError = touched && !name.trim() ? 'A server name is required' : undefined
  const intervalError =
    interval < 1 || interval > 3600 ? 'Must be between 1 and 3600 seconds' : undefined
  const retriesError = retries < 1 || retries > 10 ? 'Must be between 1 and 10' : undefined
  const canSubmit = !busy && name.trim() !== '' && !intervalError && !retriesError

  const submit = async () => {
    setTouched(true)
    if (!name.trim() || intervalError || retriesError) return
    setError(null)
    try {
      const result = await create({
        name: name.trim(),
        os_type: osType,
        check_interval: interval,
        retry_attempts: retries,
      })
      setCreated(result)
      onCreated()
      push(`Agent "${result.agent.name}" registered`, 'success')
    } catch (err) {
      setError((err as ApiError).message || 'Could not register the agent')
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Add server agent"
        className="flex max-h-[90vh] w-full max-w-3xl flex-col rounded-xl border border-white/10 bg-slate-900/95"
      >
        <div className="flex shrink-0 items-start justify-between gap-4 border-b border-white/10 p-6">
          <div>
            <h2 className="text-2xl font-light text-white">
              {created ? 'Install the agent' : 'Add Server Agent'}
            </h2>
            <p className="mt-1 text-sm text-slate-400">
              {created
                ? `Run one of these on ${created.agent.name} to start reporting`
                : 'Register a host, then install the agent on it'}
            </p>
          </div>
          <button
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 text-slate-400 transition hover:text-white"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
          {!created ? (
            <ConfigureStep
              firstFieldRef={firstFieldRef}
              name={name}
              setName={setName}
              nameError={nameError}
              osType={osType}
              setOSType={setOSType}
              interval={interval}
              setInterval={setInterval}
              intervalError={intervalError}
              retries={retries}
              setRetries={setRetries}
              retriesError={retriesError}
              error={error}
            />
          ) : (
            <InstallStep created={created} tab={tab} setTab={setTab} />
          )}
        </div>

        <div className="flex shrink-0 items-center justify-end gap-3 border-t border-white/10 p-6">
          <button
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-white/20 px-6 py-2 text-sm font-medium text-white transition hover:bg-white/5 disabled:opacity-50"
          >
            {created ? 'Done' : 'Cancel'}
          </button>
          {!created && (
            <button
              onClick={() => void submit()}
              disabled={!canSubmit}
              className="inline-flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-600 to-emerald-500 px-6 py-2 text-sm font-medium text-white transition hover:from-emerald-500 hover:to-emerald-400 disabled:opacity-50"
            >
              {busy && <Loader2 className="h-4 w-4 animate-spin" />}
              Create Server Agent
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function ConfigureStep(props: {
  firstFieldRef: React.RefObject<HTMLInputElement>
  name: string
  setName: (v: string) => void
  nameError?: string
  osType: AgentOS
  setOSType: (v: AgentOS) => void
  interval: number
  setInterval: (v: number) => void
  intervalError?: string
  retries: number
  setRetries: (v: number) => void
  retriesError?: string
  error: string | null
}) {
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
              ref={props.firstFieldRef}
              value={props.name}
              onChange={(e) => props.setName(e.target.value)}
              placeholder="e.g. web-server-01"
              className={`${field} ${props.nameError ? 'border-red-500/60' : ''}`}
            />
            <p className={`mt-1 text-xs ${props.nameError ? 'text-red-400' : 'text-slate-500'}`}>
              {props.nameError ?? 'How this host appears in Sentinel'}
            </p>
          </div>

          <div>
            <label htmlFor="agent-os" className="mb-1 block text-sm font-medium text-white">
              Operating System
            </label>
            <select
              id="agent-os"
              value={props.osType}
              onChange={(e) => props.setOSType(e.target.value as AgentOS)}
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
                value={props.interval}
                onChange={(e) => props.setInterval(Number(e.target.value))}
                className={`w-32 ${field} ${props.intervalError ? 'border-red-500/60' : ''}`}
              />
              <span className="text-sm text-slate-400">seconds</span>
            </div>
            <p className={`mt-1 text-xs ${props.intervalError ? 'text-red-400' : 'text-slate-500'}`}>
              {props.intervalError ?? 'How often metrics are collected'}
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
              value={props.retries}
              onChange={(e) => props.setRetries(Number(e.target.value))}
              className={`w-32 ${field} ${props.retriesError ? 'border-red-500/60' : ''}`}
            />
            <p className={`mt-1 text-xs ${props.retriesError ? 'text-red-400' : 'text-slate-500'}`}>
              {props.retriesError ?? 'Before a submission is queued for later'}
            </p>
          </div>
        </div>
      </section>

      <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
        <div className="flex items-start gap-3">
          <ServerCog className="mt-0.5 h-4 w-4 shrink-0 text-slate-400" aria-hidden />
          <p className="text-xs text-slate-400">
            The agent id and access token are generated when you create the agent. The token is
            shown once here — it can be read again later from this agent&apos;s install
            instructions, but it is never listed alongside the other agents.
          </p>
        </div>
      </div>

      {props.error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
          {props.error}
        </div>
      )}
    </>
  )
}

function InstallStep({
  created,
  tab,
  setTab,
}: {
  created: CreatedAgent
  tab: Tab
  setTab: (t: Tab) => void
}) {
  const { agent, sentinel_url: url } = created
  const env = [
    `SERVER_TOKEN="${agent.server_token ?? ''}"`,
    `AGENT_ID="${agent.agent_id}"`,
    `SENTINEL_URL="${url}"`,
    `SERVER_NAME="${agent.name}"`,
    `OS_TYPE="${agent.os_type}"`,
    `CHECK_INTERVAL="${agent.check_interval}"`,
    `RETRY_ATTEMPTS="${agent.retry_attempts}"`,
  ].join(' \\\n  ')

  const bashInstall = `curl -L -o server-agent.sh "${url}/scripts/server-agent.sh"
chmod +x server-agent.sh
${env} \\
  sudo -E bash ./server-agent.sh`

  const dockerInstall = `curl -L -o server-docker-agent.sh "${url}/scripts/server-docker-agent.sh"
chmod +x server-docker-agent.sh
${env} \\
  sudo -E bash ./server-docker-agent.sh`

  const directDocker = `# Build the agent image from the binary this Sentinel serves
curl -L -o sentinel-agent "${url}/agent/download/linux/amd64"
chmod +x sentinel-agent
printf 'FROM alpine:latest\\nRUN apk --no-cache add ca-certificates\\nCOPY sentinel-agent /usr/local/bin/sentinel-agent\\nENTRYPOINT ["/usr/local/bin/sentinel-agent"]\\n' > Dockerfile
docker build -t sentinel-agent:local .

docker run -d \\
  --name sentinel-agent \\
  --restart unless-stopped \\
  --network host \\
  --pid host \\
  -v /proc:/host/proc:ro \\
  -v /etc/os-release:/host/etc/os-release:ro \\
  -v /etc/hostname:/host/etc/hostname:ro \\
  -v /var/run/docker.sock:/var/run/docker.sock:ro \\
  -v /:/hostfs:ro \\
  -e HOST_PROC=/host/proc \\
  -e HOST_ETC=/host/etc \\
  -e DISK_PATH=/hostfs \\
  -e SENTINEL_URL="${url}" \\
  -e AGENT_ID="${agent.agent_id}" \\
  -e SERVER_TOKEN="${agent.server_token ?? ''}" \\
  -e SERVER_NAME="${agent.name}" \\
  -e CHECK_INTERVAL="${agent.check_interval}" \\
  -e RETRY_ATTEMPTS="${agent.retry_attempts}" \\
  sentinel-agent:local`

  return (
    <>
      <div className="grid gap-3 rounded-lg border border-white/10 bg-slate-800/40 p-4 sm:grid-cols-2">
        <div>
          <p className="text-xs text-slate-500">Agent ID</p>
          <p className="font-mono text-sm text-white">{agent.agent_id}</p>
        </div>
        <div className="min-w-0">
          <p className="text-xs text-slate-500">Server Token</p>
          <p className="truncate font-mono text-sm text-white" title={agent.server_token}>
            {agent.server_token}
          </p>
        </div>
      </div>

      {/* Horizontally scrollable so four tabs do not wrap on a phone. */}
      <div className="-mx-1 flex gap-1 overflow-x-auto border-b border-white/10 px-1">
        {TABS.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`whitespace-nowrap border-b-2 px-3 py-2 text-sm transition ${
              tab === t
                ? 'border-emerald-500 text-white'
                : 'border-transparent text-slate-400 hover:text-slate-200'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'One-Click Install' && (
        <div className="space-y-4">
          <p className="text-sm text-slate-400">
            Installs the agent as a systemd service and starts it. Needs <code>curl</code>,{' '}
            <code>sudo</code> and systemd.
          </p>
          <Command label="Run on the server" value={bashInstall} />
        </div>
      )}

      {tab === 'Docker One-Click' && (
        <div className="space-y-4">
          <p className="text-sm text-slate-400">
            Runs the agent in a container with the host mounted read-only. Needs Docker and
            access to its socket.
          </p>
          <Command label="Run on the server" value={dockerInstall} />
        </div>
      )}

      {tab === 'Direct Docker Run' && (
        <div className="space-y-4">
          <p className="text-sm text-slate-400">
            Every step the Docker installer performs, if you would rather run them yourself.
            Change <code>amd64</code> to <code>arm64</code> on ARM hardware.
          </p>
          <Command value={directDocker} />
        </div>
      )}

      {tab === 'Manual' && (
        <div className="space-y-4">
          <div>
            <h4 className="mb-2 text-sm font-medium text-white">Prerequisites</h4>
            <ul className="list-inside list-disc space-y-1 text-sm text-slate-400">
              <li>A 64-bit Linux host (x86-64 or ARM64)</li>
              <li>Network access from the host to {url}</li>
              <li>Root, to install the binary and the service</li>
              <li>Docker, only if you want container metrics as well as host metrics</li>
            </ul>
          </div>
          <Command
            label="1. Download the binary"
            value={`curl -L -o sentinel-agent "${url}/agent/download/linux/amd64"`}
          />
          <Command
            label="2. Install it"
            value={`chmod +x sentinel-agent\nsudo mv sentinel-agent /usr/local/bin/`}
          />
          <Command
            label="3. Run it"
            value={`${env} \\\n  /usr/local/bin/sentinel-agent`}
          />
          <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
            <h4 className="mb-2 text-sm font-medium text-white">After installation</h4>
            <ul className="list-inside list-disc space-y-1 text-xs text-slate-400">
              <li>The host appears under Server Monitoring within a minute.</li>
              <li>
                Metrics are sent every {agent.check_interval} seconds; a heartbeat every 5
                minutes keeps it marked active.
              </li>
              <li>
                If the server is unreachable the agent queues samples and sends them when it
                returns.
              </li>
              <li>Running the installer again upgrades an existing install in place.</li>
            </ul>
          </div>
        </div>
      )}
    </>
  )
}
