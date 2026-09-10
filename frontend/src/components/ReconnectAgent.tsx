import { useState } from 'react'
import { Loader2, Plug, Copy, Check } from 'lucide-react'
import SettingsCard from '@/components/SettingsCard'
import api, { type ApiError } from '@/services/api'
import type { Agent } from '@/hooks/useAgents'

interface ReconnectResponse {
  agent: Agent
  /** True when the agent had been deleted and its registration was restored. */
  recreated: boolean
  internal_url: string
}

/** The shape an agent id takes, checked here so an obvious typo is caught. */
const AGENT_ID_PATTERN = /^agent_[0-9a-f]{10}$/

function CopyBox({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      // Refused outside a secure context, which a LAN install over plain http
      // is. Falling back keeps the button working.
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
      <p className="mb-1 text-xs font-medium text-slate-400">{label}</p>
      <div className="relative">
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
 * Issues a fresh token for an agent that is still installed on its host.
 *
 * Covers two situations with one action, because from the host's side they are
 * the same: the registration was deleted here while the agent kept running, or
 * the agent has the wrong token. Either way the host still knows its own id,
 * and that is enough to reconnect it without reinstalling anything.
 */
export default function ReconnectAgent({
  push,
  onReconnected,
}: {
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
  onReconnected?: () => void
}) {
  const [agentID, setAgentID] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ReconnectResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const trimmed = agentID.trim()
  const idError =
    trimmed !== '' && !AGENT_ID_PATTERN.test(trimmed)
      ? 'An agent id looks like agent_1a2b3c4d5e'
      : undefined
  const canSubmit = !busy && trimmed !== '' && !idError

  const submit = async () => {
    if (!canSubmit) return
    setError(null)
    setBusy(true)
    try {
      const res = await api.post<{ data: ReconnectResponse }>(
        `/agents/${trimmed}/reconnect`,
        { name: name.trim() },
      )
      setResult(res.data.data)
      push(res.data.data.recreated ? 'Agent re-registered' : 'New token issued', 'success')
      onReconnected?.()
    } catch (err) {
      setError((err as ApiError).message || 'Could not reconnect that agent')
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsCard
      title="Reconnect an Agent"
      description="Issues a new token for a host that still has the agent installed — after deleting a server by mistake, or when a host has the wrong token."
    >
      <label htmlFor="reconnect-id" className="block text-sm font-medium text-white">
        Agent ID
      </label>
      <input
        id="reconnect-id"
        value={agentID}
        onChange={(e) => setAgentID(e.target.value)}
        placeholder="agent_1a2b3c4d5e"
        className={`w-full font-mono ${idError ? 'border-red-500/60' : ''}`}
      />
      <p className={`text-xs ${idError ? 'text-red-400' : 'text-slate-500'}`}>
        {idError ??
          'Find it on the host: /etc/sentinel/agent.conf, or `docker inspect sentinel-agent`.'}
      </p>

      <label htmlFor="reconnect-name" className="mt-2 block text-sm font-medium text-white">
        Server name <span className="font-normal text-slate-500">(optional)</span>
      </label>
      <input
        id="reconnect-name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Used only if the server has to be re-registered"
        className="w-full"
      />

      <div className="pt-1">
        <button className="btn-primary !py-1.5" onClick={() => void submit()} disabled={!canSubmit}>
          {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plug className="h-4 w-4" />}
          Generate New Token
        </button>
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {result && (
        <div className="space-y-3 rounded-lg border border-white/10 bg-slate-800/40 p-4">
          <p className="text-sm text-slate-300">
            {result.recreated
              ? 'That server had been deleted, so its registration was restored under the same id. Its previous metric history is not recoverable.'
              : 'The token for that server has been replaced. Its previous token no longer works.'}
          </p>

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <p className="text-xs text-slate-500">Agent ID (unchanged)</p>
              <p className="font-mono text-sm text-white">{result.agent.agent_id}</p>
            </div>
            <div className="min-w-0">
              <p className="text-xs text-slate-500">New server token</p>
              <p className="truncate font-mono text-sm text-white" title={result.agent.server_token}>
                {result.agent.server_token}
              </p>
            </div>
          </div>

          {/* The commands rather than a description of them: this is done over
              SSH under time pressure, and a paraphrase has to be translated
              back into exactly this. */}
          <CopyBox
            label="On the host, if it was installed with the systemd installer"
            value={`sudo sed -i 's|^SERVER_TOKEN=.*|SERVER_TOKEN=${result.agent.server_token}|' /etc/sentinel/agent.conf
sudo systemctl restart sentinel-agent
sudo journalctl -u sentinel-agent -n 20 --no-pager`}
          />
          <CopyBox
            label="On the host, if it is running in Docker"
            value={`docker rm -f sentinel-agent
docker run -d --name sentinel-agent --restart unless-stopped \\
  --network host --pid host \\
  -v /proc:/host/proc:ro -v /etc/os-release:/host/etc/os-release:ro \\
  -v /etc/hostname:/host/etc/hostname:ro \\
  -v /var/run/docker.sock:/var/run/docker.sock:ro -v /:/hostfs:ro \\
  -e HOST_PROC=/host/proc -e HOST_ETC=/host/etc -e DISK_PATH=/hostfs \\
  -e SENTINEL_URL="${result.internal_url}" \\
  -e AGENT_ID="${result.agent.agent_id}" \\
  -e SERVER_TOKEN="${result.agent.server_token}" \\
  sentinel-agent:local`}
          />
          <p className="text-xs text-slate-500">
            The host appears under Server Monitoring within a minute of restarting.
          </p>
        </div>
      )}
    </SettingsCard>
  )
}
