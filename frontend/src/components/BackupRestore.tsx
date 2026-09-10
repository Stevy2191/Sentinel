import { useState } from 'react'
import { Database, Download, Trash2, RotateCcw, Loader2, AlertTriangle, Check } from 'lucide-react'
import SettingsCard from '@/components/SettingsCard'
import { useBackups, useBackupActions, type Backup, type RestoreResult } from '@/hooks/useBackups'
import type { ApiError } from '@/services/api'

function size(mb: number): string {
  if (mb < 1) return `${Math.max(1, Math.round(mb * 1024))} KB`
  return `${mb.toFixed(1)} MB`
}

function when(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

/**
 * Backup and restore, on Settings → System.
 *
 * Restore is the only action in the application that destroys data outright,
 * so it is deliberately the slowest thing here to do by accident: a separate
 * confirmation naming the backup, and a safety copy taken by the server before
 * anything is replaced.
 */
export default function BackupRestore({
  push,
}: {
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const { backups, directory, unavailable, loading, error, refetch } = useBackups()
  const { create, restore, remove, download, busy } = useBackupActions()

  const [confirmRestore, setConfirmRestore] = useState<Backup | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Backup | null>(null)
  const [restoring, setRestoring] = useState(false)
  const [restored, setRestored] = useState<RestoreResult | null>(null)

  const latest = backups[0]

  const doCreate = async () => {
    try {
      const b = await create()
      push(`Backup created (${size(b.size_mb)})`, 'success')
      await refetch()
    } catch (err) {
      push((err as ApiError).message || 'Backup failed', 'error')
    }
  }

  const doRestore = async () => {
    if (!confirmRestore) return
    setRestoring(true)
    try {
      const result = await restore(confirmRestore.backup_id)
      setRestored(result)
      setConfirmRestore(null)
    } catch (err) {
      push((err as ApiError).message || 'Restore failed', 'error')
      setConfirmRestore(null)
    } finally {
      setRestoring(false)
    }
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    try {
      await remove(confirmDelete.backup_id)
      push('Backup deleted', 'success')
      setConfirmDelete(null)
      await refetch()
    } catch (err) {
      push((err as ApiError).message || 'Could not delete the backup', 'error')
    }
  }

  const doDownload = async (b: Backup) => {
    try {
      await download(b)
    } catch (err) {
      push((err as ApiError).message || 'Download failed', 'error')
    }
  }

  return (
    <>
      <SettingsCard
        title="Database Backup & Restore"
        description="A backup captures the whole database: monitors, incidents, history, users and settings."
      >
        {unavailable ? (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-300">
            <p className="font-medium">Backups are not available on this server.</p>
            <p className="mt-1 text-xs">{unavailable}</p>
          </div>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-3">
              <button className="btn-primary !py-1.5" onClick={() => void doCreate()} disabled={busy}>
                {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Database className="h-4 w-4" />}
                Create Backup Now
              </button>
              <span className="text-xs text-slate-500">
                {latest
                  ? `Last backup: ${when(latest.created_at)} (${size(latest.size_mb)})`
                  : 'No backups yet'}
              </span>
            </div>

            {/* Said plainly rather than left to be discovered: a downloaded
                backup is a copy of every credential in the database. */}
            <p className="text-xs text-slate-500">
              Stored in <code>{directory}</code>, readable only by the server. A backup contains
              password hashes, notification channel secrets and agent tokens — treat a downloaded
              file as you would those.
            </p>

            {error && (
              <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
                {error}
              </div>
            )}

            {loading ? (
              <div className="flex items-center gap-2 text-sm text-slate-400">
                <Loader2 className="h-4 w-4 animate-spin" /> Loading backups…
              </div>
            ) : backups.length === 0 ? (
              <p className="rounded-lg border border-white/10 bg-slate-800/40 p-6 text-center text-sm text-slate-500">
                No backups yet. Create one before making changes you might want to undo.
              </p>
            ) : (
              <div className="overflow-hidden rounded-lg border border-white/10">
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-white/10 bg-slate-800/30 text-xs font-medium text-slate-400">
                        <th className="px-4 py-2 text-left">Created</th>
                        <th className="px-4 py-2 text-left">Size</th>
                        <th className="px-4 py-2 text-left">Schema</th>
                        <th className="px-4 py-2 text-right">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-white/5">
                      {backups.map((b) => (
                        <tr key={b.backup_id}>
                          <td className="whitespace-nowrap px-4 py-2 text-slate-200">
                            {when(b.created_at)}
                            <div className="max-w-[260px] truncate font-mono text-xs text-slate-600">
                              {b.filename}
                            </div>
                          </td>
                          <td className="whitespace-nowrap px-4 py-2 tabular-nums text-slate-400">
                            {size(b.size_mb)}
                          </td>
                          <td className="px-4 py-2 font-mono text-xs text-slate-500">
                            {b.schema_version ? b.schema_version.slice(0, 3) : '—'}
                          </td>
                          <td className="px-4 py-2">
                            <div className="flex items-center justify-end gap-1">
                              <button
                                className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
                                title="Download"
                                aria-label={`Download ${b.filename}`}
                                onClick={() => void doDownload(b)}
                              >
                                <Download className="h-4 w-4" />
                              </button>
                              <button
                                className="rounded p-1.5 text-amber-400 transition hover:bg-amber-500/10 hover:text-amber-300"
                                title="Restore"
                                aria-label={`Restore ${b.filename}`}
                                onClick={() => setConfirmRestore(b)}
                              >
                                <RotateCcw className="h-4 w-4" />
                              </button>
                              <button
                                className="rounded p-1.5 text-red-400 transition hover:bg-red-500/10 hover:text-red-300"
                                title="Delete"
                                aria-label={`Delete ${b.filename}`}
                                onClick={() => setConfirmDelete(b)}
                              >
                                <Trash2 className="h-4 w-4" />
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </>
        )}
      </SettingsCard>

      {confirmRestore && (
        <Dialog onClose={() => !restoring && setConfirmRestore(null)}>
          <h3 className="text-lg font-semibold text-white">Restore this backup?</h3>
          <div className="mt-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">
            <div className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              <p>
                Every monitor, incident, user and setting is replaced with the contents of this
                backup. Anything recorded since it was taken is lost.
              </p>
            </div>
          </div>
          <dl className="mt-4 space-y-1 text-sm">
            <div className="flex justify-between gap-4">
              <dt className="text-slate-500">Taken</dt>
              <dd className="text-slate-200">{when(confirmRestore.created_at)}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-slate-500">Size</dt>
              <dd className="text-slate-200">{size(confirmRestore.size_mb)}</dd>
            </div>
          </dl>
          <p className="mt-3 text-xs text-slate-500">
            A backup of the current data is taken first, so this can be undone.
          </p>
          <div className="mt-6 flex justify-end gap-2">
            <button
              className="btn-secondary"
              onClick={() => setConfirmRestore(null)}
              disabled={restoring}
            >
              Cancel
            </button>
            <button
              className="inline-flex items-center gap-2 rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500 disabled:opacity-50"
              onClick={() => void doRestore()}
              disabled={restoring}
            >
              {restoring && <Loader2 className="h-4 w-4 animate-spin" />}
              {restoring ? 'Restoring…' : 'I understand, restore'}
            </button>
          </div>
        </Dialog>
      )}

      {restored && (
        <Dialog onClose={() => window.location.reload()}>
          <h3 className="flex items-center gap-2 text-lg font-semibold text-white">
            <Check className="h-5 w-5 text-emerald-400" /> Database restored
          </h3>
          <p className="mt-2 text-sm text-slate-400">{restored.message}</p>
          {restored.safety_backup_id ? (
            <p className="mt-3 rounded-lg border border-white/10 bg-slate-800/40 p-3 text-xs text-slate-400">
              The data replaced was saved first, as backup{' '}
              <span className="font-mono text-slate-200">{restored.safety_backup_id}</span>.
              Restoring that one undoes this.
            </p>
          ) : (
            <p className="mt-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-300">
              The safety copy could not be taken: {restored.safety_backup_error}
            </p>
          )}
          <div className="mt-6 flex justify-end">
            {/* Reloaded rather than left as it was: everything on screen was
                read from the database that has just been replaced. */}
            <button className="btn-primary" onClick={() => window.location.reload()}>
              Reload Sentinel
            </button>
          </div>
        </Dialog>
      )}

      {confirmDelete && (
        <Dialog onClose={() => setConfirmDelete(null)}>
          <h3 className="text-lg font-semibold text-white">Delete this backup?</h3>
          <p className="mt-2 text-sm text-slate-400">
            {when(confirmDelete.created_at)} · {size(confirmDelete.size_mb)}. The file is removed
            from the server and cannot be recovered.
          </p>
          <div className="mt-6 flex justify-end gap-2">
            <button className="btn-secondary" onClick={() => setConfirmDelete(null)} disabled={busy}>
              Cancel
            </button>
            <button
              className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500 disabled:opacity-50"
              onClick={() => void doDelete()}
              disabled={busy}
            >
              {busy ? 'Deleting…' : 'Delete'}
            </button>
          </div>
        </Dialog>
      )}
    </>
  )
}

function Dialog({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6"
      >
        {children}
      </div>
    </div>
  )
}
