import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type {
  CreateToolRunRequest,
  NetToolsSettings,
  ToolRun,
  ToolRunDetail,
  ToolRunFilter,
  VantagesResponse,
} from '@/types/netTools'
import { HISTORY_PAGE_SIZE } from '@/utils/netTools'

function message(err: unknown, fallback: string): string {
  return (err as ApiError)?.message || fallback
}

interface Resource<T> {
  data: T | null
  setData: (d: T) => void
  loading: boolean
  error: string | null
  reload: () => void
}

/**
 * Loads the resource named by key (null: nothing to load), optionally
 * polling. A response is applied only if it answers the latest request, so
 * a slow answer for an old filter or run never replaces a newer one, and
 * nothing lands after unmount (CLAUDE.md: guard async results). Only the
 * first load for a key shows as loading; polls refresh quietly.
 */
function useResource<T>(
  key: string | null,
  fetcher: (key: string) => Promise<T>,
  fallback: string,
  pollMs = 0,
): Resource<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(key !== null)
  const [error, setError] = useState<string | null>(null)
  const latest = useRef(0)

  const reload = useCallback(() => {
    if (key === null) return
    latest.current += 1
    const mine = latest.current
    fetcher(key)
      .then(
        (d) => {
          if (mine !== latest.current) return
          setData(d)
          setError(null)
        },
        (err) => {
          if (mine === latest.current) setError(message(err, fallback))
        },
      )
      .finally(() => {
        if (mine === latest.current) setLoading(false)
      })
  }, [key, fetcher, fallback])

  useEffect(() => {
    setData(null)
    setError(null)
    setLoading(key !== null)
    reload()
    const timer = key !== null && pollMs > 0 ? window.setInterval(reload, pollMs) : 0
    return () => {
      // Whatever is still in flight belongs to the old key: drop it.
      latest.current += 1
      if (timer) window.clearInterval(timer)
    }
  }, [key, reload, pollMs])

  return { data, setData, loading, error, reload }
}

const VANTAGES_POLL_MS = 15_000
const HISTORY_POLL_MS = 15_000

async function fetchVantages(): Promise<VantagesResponse> {
  const res = await api.get<ApiResponse<VantagesResponse>>('/tools/vantages')
  return res.data.data
}

/** Where tools can run from, and whether the allowlist is empty. Polled,
 *  because an agent's readiness follows its polling. */
export function useVantages(): {
  data: VantagesResponse | null
  loading: boolean
  error: string | null
  reload: () => void
} {
  const r = useResource('vantages', fetchVantages, 'Could not load where tools can run from', VANTAGES_POLL_MS)
  return { data: r.data, loading: r.loading, error: r.error, reload: r.reload }
}

async function fetchRuns(key: string): Promise<{ runs: ToolRun[]; total: number }> {
  const f = JSON.parse(key) as ToolRunFilter
  const params: Record<string, string | number> = {
    limit: f.limit ?? HISTORY_PAGE_SIZE,
    offset: f.offset ?? 0,
  }
  if (f.tool) params.tool = f.tool
  if (f.status) params.status = f.status
  if (f.mine) params.mine = 1
  if (f.agent_id) params.agent_id = f.agent_id
  if (f.target) params.target = f.target
  const res = await api.get<ApiResponse<ToolRun[]>>('/tools/runs', { params })
  const runs = res.data.data ?? []
  const total = Number(res.headers['x-total-count'])
  return { runs, total: Number.isFinite(total) ? total : runs.length }
}

/** Run history, newest first, refreshed every 15 s (a team's runs). */
export function useToolRuns(filter: ToolRunFilter): {
  runs: ToolRun[]
  total: number
  loading: boolean
  error: string | null
  reload: () => void
} {
  const r = useResource(JSON.stringify(filter), fetchRuns, 'Could not load the run history', HISTORY_POLL_MS)
  return { runs: r.data?.runs ?? [], total: r.data?.total ?? 0, loading: r.loading, error: r.error, reload: r.reload }
}

async function fetchRun(id: string): Promise<ToolRunDetail> {
  const res = await api.get<ApiResponse<ToolRunDetail>>(`/tools/runs/${encodeURIComponent(id)}`)
  return res.data.data
}

/** One run with every stored event. Live progress comes from useRunStream. */
export function useToolRun(id: string | undefined): {
  detail: ToolRunDetail | null
  loading: boolean
  error: string | null
  reload: () => void
} {
  const r = useResource(id ?? null, fetchRun, 'Could not load the run')
  return { detail: r.data, loading: r.loading, error: r.error, reload: r.reload }
}

export async function createToolRun(req: CreateToolRunRequest): Promise<ToolRun> {
  const res = await api.post<ApiResponse<ToolRun>>('/tools/runs', req)
  return res.data.data
}

export async function cancelToolRun(id: string): Promise<ToolRun> {
  const res = await api.post<ApiResponse<ToolRun>>(`/tools/runs/${encodeURIComponent(id)}/cancel`)
  return res.data.data
}

async function fetchSettings(): Promise<NetToolsSettings> {
  const res = await api.get<ApiResponse<NetToolsSettings>>('/settings/net-tools')
  return res.data.data
}

/** Settings → Network tools (admin only). save throws the ApiError of a
 *  refused save; a bad allowlist's entries are in its `details`. */
export function useNetToolsSettings(): {
  settings: NetToolsSettings | null
  loading: boolean
  error: string | null
  save: (s: NetToolsSettings) => Promise<NetToolsSettings>
  reload: () => void
} {
  const r = useResource('net-tools-settings', fetchSettings, 'Could not load the network tools settings')
  const { setData } = r
  const save = useCallback(
    async (s: NetToolsSettings) => {
      const res = await api.put<ApiResponse<NetToolsSettings>>('/settings/net-tools', s)
      setData(res.data.data)
      return res.data.data
    },
    [setData],
  )
  return { settings: r.data, loading: r.loading, error: r.error, save, reload: r.reload }
}

/** Grants or removes a user's network tools permission (admin only). */
export async function setUserNetTools(userId: string, enabled: boolean): Promise<void> {
  await api.patch(`/users/${encodeURIComponent(userId)}/net-tools`, { enabled })
}

/** Sentinel's switch for running tools from an agent (admin only). */
export async function setAgentTools(agentId: string, enabled: boolean): Promise<void> {
  await api.put(`/agents/${encodeURIComponent(agentId)}/tools`, { enabled })
}
