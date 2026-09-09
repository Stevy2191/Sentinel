import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'

export type AgentStatus = 'pending' | 'active' | 'offline'
export type AgentOS = 'ubuntu' | 'debian' | 'centos' | 'rhel' | 'windows' | 'linux'

export interface Agent {
  id: string
  name: string
  agent_id: string
  /** Only present on creation and on the admin single-agent fetch. */
  server_token?: string
  os_type: AgentOS
  check_interval: number
  retry_attempts: number
  status: AgentStatus
  last_heartbeat: string | null
  ip_address: string | null
  hostname: string | null
  os_version: string | null
  agent_version: string | null
  created_at: string
  updated_at: string
}

export interface AgentMetric {
  id: number
  timestamp: string
  cpu_percent: number | null
  memory_percent: number | null
  memory_used_mb: number | null
  memory_total_mb: number | null
  disk_percent: number | null
  disk_used_gb: number | null
  disk_total_gb: number | null
  uptime_seconds: number | null
  load_average_1m: number | null
  load_average_5m: number | null
  load_average_15m: number | null
  network_in_bytes: number | null
  network_out_bytes: number | null
}

export interface AgentContainer {
  id: number
  container_id: string
  container_name: string
  image: string
  status: string
  cpu_percent: number
  memory_percent: number
  memory_used_mb: number
  timestamp: string
}

export interface AgentStatusDetail {
  agent: Agent
  latest: AgentMetric | null
  containers: AgentContainer[]
}

export interface CreateAgentInput {
  name: string
  os_type: AgentOS
  check_interval: number
  retry_attempts: number
}

/** What the server returns when an agent is registered. */
export interface CreatedAgent {
  agent: Agent
  sentinel_url: string
}

const BASE = '/agents'

function message(err: unknown, fallback: string): string {
  return (err as ApiError)?.message || fallback
}

/**
 * Lists agents, polling so a host that has just been installed appears without
 * the page being reloaded — which is exactly what someone is watching for
 * after running an install command.
 */
export function useAgents(pollMs = 15000) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    try {
      const res = await api.get<{ data: Agent[] }>(BASE)
      setAgents(res.data.data ?? [])
      setError(null)
    } catch (err) {
      setError(message(err, 'Could not load agents'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
    if (pollMs <= 0) return
    const t = window.setInterval(() => void refetch(), pollMs)
    return () => window.clearInterval(t)
  }, [refetch, pollMs])

  return { agents, loading, error, refetch }
}

/** One agent's current state and latest metrics. */
export function useAgentStatus(agentID: string | null, pollMs = 15000) {
  const [detail, setDetail] = useState<AgentStatusDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    if (!agentID) return
    try {
      const res = await api.get<{ data: AgentStatusDetail }>(`${BASE}/${agentID}/status`)
      setDetail(res.data.data)
      setError(null)
    } catch (err) {
      setError(message(err, 'Could not load agent status'))
    } finally {
      setLoading(false)
    }
  }, [agentID])

  useEffect(() => {
    if (!agentID) {
      setDetail(null)
      return
    }
    setLoading(true)
    void refetch()
    if (pollMs <= 0) return
    const t = window.setInterval(() => void refetch(), pollMs)
    return () => window.clearInterval(t)
  }, [agentID, refetch, pollMs])

  return { detail, loading, error, refetch }
}

/** Historical metrics for charting. */
export function useAgentMetrics(agentID: string | null, duration = '1h') {
  const [metrics, setMetrics] = useState<AgentMetric[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!agentID) {
      setMetrics([])
      return
    }
    let active = true
    setLoading(true)
    api
      .get<{ data: { metrics: AgentMetric[] } }>(`${BASE}/${agentID}/metrics?duration=${duration}`)
      .then((res) => {
        if (!active) return
        // Oldest first, which is the order a chart reads.
        setMetrics([...(res.data.data.metrics ?? [])].reverse())
      })
      .catch(() => active && setMetrics([]))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [agentID, duration])

  return { metrics, loading }
}

export function useAgentActions() {
  const [busy, setBusy] = useState(false)

  const create = useCallback(async (input: CreateAgentInput): Promise<CreatedAgent> => {
    setBusy(true)
    try {
      const res = await api.post<{ data: CreatedAgent }>(BASE, input)
      return res.data.data
    } finally {
      setBusy(false)
    }
  }, [])

  /** Re-reads an agent including its token, to rebuild install instructions. */
  const getWithToken = useCallback(async (agentID: string): Promise<CreatedAgent> => {
    const res = await api.get<{ data: CreatedAgent }>(`${BASE}/${agentID}`)
    return res.data.data
  }, [])

  const update = useCallback(
    async (agentID: string, patch: Partial<CreateAgentInput>): Promise<Agent> => {
      setBusy(true)
      try {
        const res = await api.patch<{ data: Agent }>(`${BASE}/${agentID}`, patch)
        return res.data.data
      } finally {
        setBusy(false)
      }
    },
    [],
  )

  const remove = useCallback(async (agentID: string) => {
    setBusy(true)
    try {
      await api.delete(`${BASE}/${agentID}`)
    } finally {
      setBusy(false)
    }
  }, [])

  return { create, getWithToken, update, remove, busy }
}
