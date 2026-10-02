import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface MibModule {
  id: string
  name: string
  source: 'builtin' | 'upload'
  file_name: string
  size_bytes: number
  imports: string[]
  missing: string[]
  status: 'ready' | 'waiting'
  updated_at: string
  objects: number
}

export interface MibObject {
  id: number
  name: string
  oid: string
  parent_oid: string
  kind: 'node' | 'scalar' | 'table' | 'row' | 'column' | 'notification'
  base_type: string
  type_name: string
  units: string
  access: string
  description: string
  enum: Record<string, string> | null
  index_columns: string[]
  module: string
  has_children: boolean
}

export interface MibObjectDetail extends MibObject {
  columns: MibObject[] | null
}

export interface MibUploadResult {
  saved: string[]
  skipped: string[]
  waiting: Record<string, string[]>
}

export interface TestWalkResult {
  oid: string
  columns: { oid: string; name: string; enum: Record<string, string> | null }[]
  rows: { index: string; values: Record<string, { raw: string; meaning: string }> }[]
  truncated: boolean
}

/** Cisco files to upload for names in the browser (Sentinel cannot ship them). */
export const CISCO_MIB_FILES = [
  'CISCO-SMI', 'CISCO-TC', 'CISCO-PROCESS-MIB', 'CISCO-MEMORY-POOL-MIB', 'CISCO-ENHANCED-MEMPOOL-MIB',
  'CISCO-ENVMON-MIB', 'CISCO-ENTITY-SENSOR-MIB', 'CISCO-ENTITY-FRU-CONTROL-MIB',
].map((name) => ({ name, url: `https://raw.githubusercontent.com/cisco/cisco-mibs/main/v2/${name}.my` }))

export function useMibModules() {
  const [modules, setModules] = useState<MibModule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<MibModule[]>>('/network/mibs')
      setModules(data.data ?? [])
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load MIB modules')
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => void refetch(), [refetch])
  return { modules, loading, error, refetch }
}

export async function uploadMibs(files: File[]): Promise<MibUploadResult> {
  const form = new FormData()
  files.forEach((f) => form.append('files', f))
  const { data } = await api.post<ApiResponse<MibUploadResult>>('/network/mibs', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return data.data
}

export async function deleteMib(id: string): Promise<void> {
  await api.delete(`/network/mibs/${id}`)
}

export async function mibChildren(oid: string): Promise<MibObject[]> {
  const { data } = await api.get<ApiResponse<MibObject[]>>('/network/mibs/objects/children', { params: { oid } })
  return data.data ?? []
}

export async function mibSearch(q: string): Promise<MibObject[]> {
  const { data } = await api.get<ApiResponse<MibObject[]>>('/network/mibs/objects/search', { params: { q } })
  return data.data ?? []
}

export async function mibObject(oid: string): Promise<MibObjectDetail> {
  const { data } = await api.get<ApiResponse<MibObjectDetail>>('/network/mibs/objects/by-oid', { params: { oid } })
  return data.data
}

export async function testWalk(deviceId: string, oid: string): Promise<{ ok: boolean; error?: string; result?: TestWalkResult }> {
  const { data } = await api.post<ApiResponse<{ ok: boolean; error?: string; result?: TestWalkResult }>>(
    `/devices/${deviceId}/test-walk`, { oid })
  return data.data
}
