import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { UnitFaceplate } from '@/hooks/usePorts'

/** One group of a faceplate layout, left to right (portmon.LayoutGroup). */
export interface LayoutGroup {
  first: number
  last: number
  kind: 'sfp' | 'rj45'
  rows: 1 | 2
  /** Two-row groups only: odd numbers on top, or the top row first. */
  numbering?: 'alternate' | 'across'
}

/** How a layout and a device's main ports disagree (portmon.Fit). */
export interface LayoutFit {
  missing: number[]
  extra: number[]
}

export interface SavedFaceplateLayout {
  id: string
  model: string
  groups: LayoutGroup[]
}

/** GET /devices/:id/faceplate-layout: what the editor opens with. */
export interface FaceplateEditorData {
  model: string
  saved: SavedFaceplateLayout | null
  builtin: LayoutGroup[] | null
  ports: number[]
  device_settings: boolean
  start: LayoutGroup[]
  devices: number
}

export interface FaceplatePreviewData {
  faceplates: UnitFaceplate[]
  fit: LayoutFit
}

/** A row of Settings → Faceplate layouts. */
export interface FaceplateLayoutRow {
  id: string | null
  model: string
  groups: LayoutGroup[]
  source: 'saved' | 'builtin'
  devices: number
  preview_device_id: string | null
  updated_at: string | null
  updated_by: string
}

function useLoad<T>(url: string | null) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const refetch = useCallback(async () => {
    if (!url) return
    try {
      const { data: res } = await api.get<ApiResponse<T>>(url)
      setData(res.data)
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [url])
  useEffect(() => {
    setLoading(true)
    void refetch()
  }, [refetch])
  return { data, loading, error, refetch }
}

/** The editor's starting state for a device (admins only). */
export function useFaceplateEditor(deviceId: string | null) {
  return useLoad<FaceplateEditorData>(deviceId ? `/devices/${deviceId}/faceplate-layout` : null)
}

/** Every saved and built-in layout (admins only). */
export function useFaceplateLayouts() {
  return useLoad<FaceplateLayoutRow[]>('/network/faceplate-layouts')
}

/** Draws the device with groups, without saving. */
export async function previewFaceplate(deviceId: string, groups: LayoutGroup[]): Promise<FaceplatePreviewData> {
  return (await api.post<ApiResponse<FaceplatePreviewData>>(`/devices/${deviceId}/faceplate-preview`, { groups })).data.data
}

/** Saves groups as the layout for the device's model. */
export async function saveFaceplateLayout(deviceId: string, groups: LayoutGroup[], clearDeviceSettings: boolean): Promise<void> {
  await api.put(`/devices/${deviceId}/faceplate-layout`, { groups, clear_device_settings: clearDeviceSettings })
}

export async function deleteFaceplateLayout(id: string): Promise<void> {
  await api.delete(`/network/faceplate-layouts/${id}`)
}

/** "1–12 SFP ×2 · 13–16 RJ45": a layout in a few words. */
export function describeGroups(groups: LayoutGroup[]): string {
  return groups
    .map((g) => {
      const ports = g.first === g.last ? `${g.first}` : `${g.first}–${g.last}`
      const rows = g.rows === 2 ? ` ×2${g.numbering === 'across' ? ' across' : ''}` : ''
      return `${ports} ${g.kind === 'sfp' ? 'SFP' : 'RJ45'}${rows}`
    })
    .join(' · ')
}
