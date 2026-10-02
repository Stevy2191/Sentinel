import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

/** A named set of custom metrics, applied to devices by sysObjectID prefix. */
export interface MetricProfile {
  id: string
  name: string
  description: string
  match_prefixes: string[]
  poll_interval_minutes: number
  builtin: boolean
  created_at: string
  updated_at: string
}

/** A profile as the list page shows it: with its metric and matching-device
 *  counts. */
export interface ProfileView extends MetricProfile {
  metrics: number
  devices: number
}

export type MetricSource = 'scalar' | 'column' | 'used_free_pct'
export type MetricKind = 'gauge' | 'counter' | 'status'
export type LabelMode = 'index' | 'column' | 'same_index' | 'pointer'
export type RuleKind = '' | 'above' | 'below' | 'not_ok'

/** One custom metric within a profile: every column of profile_metrics, in
 *  the backend's own snake_case json tags. */
export interface ProfileMetric {
  id: string
  profile_id: string
  name: string
  key: string
  source: MetricSource
  kind: MetricKind
  units: string
  scale: number
  oid: string
  oid2: string
  precision_oid: string
  filter_oid: string
  filter_values: string[]
  label_mode: LabelMode
  label_oid: string
  label_pointer_oid: string
  label_target_oid: string
  ok_states: number[]
  state_names: Record<string, string> | null
  rule_kind: RuleKind
  rule_value: number | null
  rule_hold_minutes: number
  rule_enabled: boolean
  position: number
  created_at: string
  updated_at: string
}

/** A profile with its metrics, in position order. */
export interface ProfileDetail extends MetricProfile {
  metrics: ProfileMetric[]
}

export interface ProfileInput {
  name: string
  description: string
  match_prefixes: string[]
  poll_interval_minutes: number
}

/** One row of a metric preview: a labelled instance with its current value,
 *  state (for a status metric) and whether it violates the metric's alert
 *  rule right now. */
export interface PreviewRow {
  instance: string
  label: string
  value: number
  state?: string
  ok: boolean
  violates: boolean
}

/** What previewing a metric against a live device returns: its rows, any
 *  per-OID read errors, and whether it is a counter (shown as a raw value
 *  here since a rate needs two polls to compute). */
export interface MetricPreview {
  rows: PreviewRow[]
  errors: Record<string, string>
  raw_counter: boolean
}

export interface PreviewResult {
  ok: boolean
  error?: string
  preview?: MetricPreview
}

/** Every profile, with its metric and matching-device counts (admin only;
 *  the API enforces it). */
export function useProfiles() {
  const [profiles, setProfiles] = useState<ProfileView[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<ProfileView[]>>('/network/profiles')
      setProfiles(data.data ?? [])
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load profiles')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => void refetch(), [refetch])
  return { profiles, loading, error, refetch }
}

/** One profile with its metrics. */
export function useProfile(id: string | undefined) {
  const [profile, setProfile] = useState<ProfileDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)

  const refetch = useCallback(async () => {
    if (!id) return
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<ProfileDetail>>(`/network/profiles/${id}`)
      setProfile(data.data)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404 || e.status === 400) setNotFound(true)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => void refetch(), [refetch])
  return { profile, loading, notFound, refetch }
}

export async function createProfile(input: ProfileInput): Promise<MetricProfile> {
  const { data } = await api.post<ApiResponse<MetricProfile>>('/network/profiles', input)
  return data.data
}

export async function updateProfile(id: string, input: ProfileInput): Promise<MetricProfile> {
  const { data } = await api.put<ApiResponse<MetricProfile>>(`/network/profiles/${id}`, input)
  return data.data
}

export async function deleteProfile(id: string): Promise<void> {
  await api.delete(`/network/profiles/${id}`)
}

export async function copyProfile(id: string): Promise<ProfileDetail> {
  const { data } = await api.post<ApiResponse<ProfileDetail>>(`/network/profiles/${id}/copy`)
  return data.data
}

/** Strips the fields the server owns (id, profile_id and the timestamps)
 *  before a metric draft goes out as a request body. A draft under
 *  construction has no real id or timestamps yet, and the server binds the
 *  body straight into a Go struct: sending an empty string for its
 *  time.Time fields fails to parse rather than simply being overwritten. */
function metricPayload(m: ProfileMetric) {
  const { id: _id, profile_id: _profileId, created_at: _createdAt, updated_at: _updatedAt, ...rest } = m
  return rest
}

export async function createMetric(profileId: string, m: ProfileMetric): Promise<ProfileMetric> {
  const { data } = await api.post<ApiResponse<ProfileMetric>>(`/network/profiles/${profileId}/metrics`, metricPayload(m))
  return data.data
}

export async function updateMetric(id: string, m: ProfileMetric): Promise<ProfileMetric> {
  const { data } = await api.put<ApiResponse<ProfileMetric>>(`/network/metrics/${id}`, metricPayload(m))
  return data.data
}

export async function deleteMetric(id: string): Promise<void> {
  await api.delete(`/network/metrics/${id}`)
}

/** How many devices have ever had data for a metric - shown before deleting
 *  one with history. */
export async function metricDataDevices(id: string): Promise<number> {
  const { data } = await api.get<ApiResponse<{ devices: number }>>(`/network/metrics/${id}/data-devices`)
  return data.data.devices
}

/** Evaluates a metric draft (not necessarily saved yet) against a live
 *  device right now, for the editor's preview button. */
export async function previewMetric(deviceId: string, m: ProfileMetric): Promise<PreviewResult> {
  const { data } = await api.post<ApiResponse<PreviewResult>>(`/devices/${deviceId}/metric-preview`, metricPayload(m))
  return data.data
}
