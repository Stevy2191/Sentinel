import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'

export interface Backup {
  backup_id: string
  filename: string
  size_bytes: number
  size_mb: number
  created_at: string
  /** The highest migration applied when the backup was taken. */
  schema_version?: string
}

export interface RestoreResult {
  message: string
  restored: string
  /** The backup taken of the current data just before it was replaced. */
  safety_backup_id?: string
  safety_backup_error?: string
}

const BASE = '/backups'

function message(err: unknown, fallback: string): string {
  return (err as ApiError)?.message || fallback
}

export function useBackups(enabled = true) {
  const [backups, setBackups] = useState<Backup[]>([])
  const [directory, setDirectory] = useState('')
  /** Why the feature cannot work, when the server tools are missing. */
  const [unavailable, setUnavailable] = useState('')
  const [loading, setLoading] = useState(enabled)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    if (!enabled) return
    try {
      const res = await api.get<{
        data: { backups: Backup[]; directory: string; unavailable: string }
      }>(BASE)
      setBackups(res.data.data.backups ?? [])
      setDirectory(res.data.data.directory ?? '')
      setUnavailable(res.data.data.unavailable ?? '')
      setError(null)
    } catch (err) {
      setError(message(err, 'Could not load backups'))
    } finally {
      setLoading(false)
    }
  }, [enabled])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { backups, directory, unavailable, loading, error, refetch }
}

export function useBackupActions() {
  const [busy, setBusy] = useState(false)

  const create = useCallback(async (): Promise<Backup> => {
    setBusy(true)
    try {
      const res = await api.post<{ data: Backup }>(`${BASE}/create`)
      return res.data.data
    } finally {
      setBusy(false)
    }
  }, [])

  const restore = useCallback(async (id: string): Promise<RestoreResult> => {
    setBusy(true)
    try {
      // The confirmation travels with the request rather than being implied by
      // the URL, so a retried or replayed POST cannot replace a database on
      // its own.
      const res = await api.post<{ data: RestoreResult }>(`${BASE}/${id}/restore`, {
        confirm: true,
      })
      return res.data.data
    } finally {
      setBusy(false)
    }
  }, [])

  const remove = useCallback(async (id: string) => {
    setBusy(true)
    try {
      await api.delete(`${BASE}/${id}`)
    } finally {
      setBusy(false)
    }
  }, [])

  /**
   * Downloads a backup through the API client so the session cookie and any
   * configured base URL apply, then hands the browser a blob. A plain link
   * would miss both.
   */
  const download = useCallback(async (backup: Backup) => {
    const res = await api.get(`${BASE}/${backup.backup_id}/download`, {
      responseType: 'blob',
    })
    const url = URL.createObjectURL(res.data as Blob)
    const a = document.createElement('a')
    a.href = url
    a.download = backup.filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    // Revoked on the next tick: released immediately, some browsers cancel the
    // download that was just started.
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }, [])

  return { create, restore, remove, download, busy }
}
