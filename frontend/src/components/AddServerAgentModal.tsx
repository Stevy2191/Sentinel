import { useEffect, useRef, useState } from 'react'
import { X, Copy, Check, Loader2, ServerCog } from 'lucide-react'
import { useAgentActions, type CreatedAgent } from '@/hooks/useAgents'
import AgentSettingsFields, {
  validateAgentSettings,
  type AgentSettings,
} from '@/components/AgentSettingsFields'
import type { ApiError } from '@/services/api'

interface Props {
  isOpen: boolean
  onClose: () => void
  onCreated: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
  /** Set to show install instructions for an agent that already exists. */
  existing?: CreatedAgent | null
}

/** A fresh form. */
const BLANK: AgentSettings = {
  name: '',
  osType: 'ubuntu',
  ipOverride: '',
  interval: 60,
  retries: 3,
}

const TABS = ['One-Click Install', 'Docker One-Click', 'Direct Docker Run', 'Manual', 'Uninstall'] as const
type Tab = (typeof TABS)[number]

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

  const [values, setValues] = useState<AgentSettings>(BLANK)
  const [touched, setTouched] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Seeded from the prop rather than only in the effect below, so opening
  // this for an existing agent paints the instructions immediately instead of
  // showing the configuration form for a frame first.
  const [created, setCreated] = useState<CreatedAgent | null>(existing ?? null)
  const [tab, setTab] = useState<Tab>('One-Click Install')

  useEffect(() => {
    if (!isOpen) return
    setValues(BLANK)
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

  const { errors, valid } = validateAgentSettings(values, touched)
  const canSubmit = !busy && valid

  const submit = async () => {
    setTouched(true)
    if (!valid) return
    setError(null)
    try {
      const result = await create({
        name: values.name.trim(),
        os_type: values.osType,
        check_interval: values.interval,
        retry_attempts: values.retries,
        ip_address_override: values.ipOverride.trim(),
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
            <>
              <AgentSettingsFields
                values={values}
                onChange={setValues}
                errors={errors}
                firstFieldRef={firstFieldRef}
              />
              {/* Only shown while creating: it explains credentials that do
                  not exist yet, which is meaningless when editing. */}
              <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
                <div className="flex items-start gap-3">
                  <ServerCog className="mt-0.5 h-4 w-4 shrink-0 text-slate-400" aria-hidden />
                  <p className="text-xs text-slate-400">
                    The agent id and access token are generated when you create the agent. The
                    token is shown once here — it can be read again later from this agent&apos;s
                    install instructions, but it is never listed alongside the other agents.
                  </p>
                </div>
              </div>
              {error && (
                <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
                  {error}
                </div>
              )}
            </>
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

function InstallStep({
  created,
  tab,
  setTab,
}: {
  created: CreatedAgent
  tab: Tab
  setTab: (t: Tab) => void
}) {
  const { agent } = created
  // Downloads come from the external address, the agent reports to the
  // internal one. They are the same unless a proxy sits in front.
  const downloadURL = created.external_url || created.sentinel_url || ''
  const reportURL = created.internal_url || downloadURL
  const proxied = reportURL !== downloadURL

  const env = [
    `SERVER_TOKEN="${agent.server_token ?? ''}"`,
    `AGENT_ID="${agent.agent_id}"`,
    `SENTINEL_URL="${reportURL}"`,
    `SERVER_NAME="${agent.name}"`,
    `OS_TYPE="${agent.os_type}"`,
    `CHECK_INTERVAL="${agent.check_interval}"`,
    `RETRY_ATTEMPTS="${agent.retry_attempts}"`,
  ].join(' \\\n  ')

  const bashInstall = `curl -L -o server-agent.sh "${downloadURL}/scripts/server-agent.sh"
chmod +x server-agent.sh
${env} \\
  sudo -E bash ./server-agent.sh`

  const dockerInstall = `curl -L -o server-docker-agent.sh "${downloadURL}/scripts/server-docker-agent.sh"
chmod +x server-docker-agent.sh
${env} \\
  sudo -E bash ./server-docker-agent.sh`

  const directDocker = `# Build the agent image from the binary this Sentinel serves
curl -L -o sentinel-agent "${downloadURL}/agent/download/linux/amd64"
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
  -e SENTINEL_URL="${reportURL}" \\
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

      {/* Only shown when the two differ, since that is the case worth
          explaining: the commands look inconsistent otherwise. */}
      {proxied && (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4 text-xs text-slate-400">
          Scripts download from <span className="text-slate-200">{downloadURL}</span> and the agent
          reports to <span className="text-slate-200">{reportURL}</span>. Both addresses must be
          reachable from the server you are installing on. Change them under Settings → System.
        </div>
      )}

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

      {tab === 'Uninstall' && (
        <div className="space-y-4">
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-xs text-amber-300">
            <p className="font-medium">Before you remove it</p>
            <ul className="mt-1 list-inside list-disc space-y-0.5">
              <li>This host stops reporting immediately; its metrics stay until you delete it here.</li>
              <li>Removing the agent does not remove it from Sentinel — it will show as offline.</li>
              <li>
                Keep the agent id <span className="font-mono">{agent.agent_id}</span> if you might
                reconnect this host later.
              </li>
            </ul>
          </div>

          <Command
            label="Installed with the systemd installer"
            value={`# Stop it and prevent it starting at boot
sudo systemctl stop sentinel-agent
sudo systemctl disable sentinel-agent

# Remove the service, binary, configuration and log
sudo rm -f /etc/systemd/system/sentinel-agent.service
sudo rm -f /usr/local/bin/sentinel-agent
sudo rm -rf /etc/sentinel
sudo rm -f /var/log/sentinel-agent.log

sudo systemctl daemon-reload
sudo systemctl reset-failed sentinel-agent 2>/dev/null || true`}
          />

          <Command
            label="Installed with the Docker installer"
            value={`# Stop and remove the container
docker rm -f sentinel-agent

# Remove the image built during installation (optional)
docker rmi sentinel-agent:local`}
          />

          <Command
            label="Verify nothing is left"
            value={`systemctl status sentinel-agent   # expect: could not be found
pgrep -a sentinel-agent           # expect: no output
docker ps -a --filter name=sentinel-agent   # expect: no rows`}
          />

          <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
            <h4 className="mb-2 text-sm font-medium text-white">Then in Sentinel</h4>
            <p className="text-xs text-slate-400">
              Delete this server under Server Monitoring to remove it and its metric history.
              Leaving it registered is also fine — it simply shows as offline. Nothing is removed
              automatically.
            </p>
          </div>
        </div>
      )}

      {tab === 'Manual' && (
        <div className="space-y-4">
          <div>
            <h4 className="mb-2 text-sm font-medium text-white">Prerequisites</h4>
            <ul className="list-inside list-disc space-y-1 text-sm text-slate-400">
              <li>A 64-bit Linux host (x86-64 or ARM64)</li>
              <li>Network access from the host to {reportURL}</li>
              <li>Root, to install the binary and the service</li>
              <li>Docker, only if you want container metrics as well as host metrics</li>
            </ul>
          </div>
          <Command
            label="1. Download the binary"
            value={`curl -L -o sentinel-agent "${downloadURL}/agent/download/linux/amd64"`}
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
