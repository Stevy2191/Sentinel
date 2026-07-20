import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, MonitorGroup } from '@/types'

const BASE = '/monitor-groups'

export interface GroupInput {
  name: string
  description?: string | null
  color?: string | null
}

/** List all monitor groups (each with monitors, count, and rolled-up uptime). */
export function useMonitorGroups() {
  const [groups, setGroups] = useState<MonitorGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<MonitorGroup[]>>(BASE)
      setGroups(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load monitor groups')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { groups, loading, error, refetch }
}

/** Create a group. */
export function useCreateMonitorGroup() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const create = useCallback(async (input: GroupInput): Promise<MonitorGroup> => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.post<ApiResponse<MonitorGroup>>(BASE, input)
      return data.data
    } catch (err) {
      setError((err as ApiError).message || 'Failed to create group')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { create, loading, error }
}

/** Update a group. */
export function useUpdateMonitorGroup() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const update = useCallback(async (groupID: string, input: GroupInput): Promise<MonitorGroup> => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.put<ApiResponse<MonitorGroup>>(`${BASE}/${groupID}`, input)
      return data.data
    } catch (err) {
      setError((err as ApiError).message || 'Failed to update group')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { update, loading, error }
}

/** Delete a group (its monitors are ungrouped). */
export function useDeleteMonitorGroup() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const remove = useCallback(async (groupID: string): Promise<void> => {
    setLoading(true)
    setError(null)
    try {
      await api.delete(`${BASE}/${groupID}`)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to delete group')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { delete: remove, loading, error }
}

/** Assign a monitor to a group, or ungroup it with groupID = null. */
export function useMoveMonitorToGroup() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const move = useCallback(async (monitorID: string, groupID: string | null): Promise<void> => {
    setLoading(true)
    setError(null)
    try {
      await api.post(`/monitors/${monitorID}/group`, { group_id: groupID })
    } catch (err) {
      setError((err as ApiError).message || 'Failed to move monitor')
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { move, loading, error }
}
