import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Play, Pause, Pencil, Trash2, Wrench, ExternalLink } from 'lucide-react'
import {
  usePauseMonitor,
  useResumeMonitor,
  useDeleteMonitor,
  useTestMonitor,
} from '@/hooks/useMonitors'
import { useDisableMaintenanceMode } from '@/hooks/useMaintenanceMode'
import ActionMenu, { type ActionItem } from '@/components/ActionMenu'
import type { MonitorAccess } from '@/utils/monitorAccess'
import type { Monitor } from '@/types'

/**
 * The per-row action menu on the monitor list.
 *
 * Rows open the monitor's page, so this exists for the things someone does
 * without wanting to leave the list — pausing a noisy monitor, ending a
 * maintenance window, running a check on the spot. Everything else lives on
 * the detail page.
 */
export default function MonitorRowActions({
  monitor,
  access,
  onChanged,
  push,
}: {
  monitor: Monitor
  access: MonitorAccess
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const navigate = useNavigate()
  const { pause, loading: pausing } = usePauseMonitor(monitor.id)
  const { resume, loading: resuming } = useResumeMonitor(monitor.id)
  const { delete: del, loading: deleting } = useDeleteMonitor(monitor.id)
  const { test, loading: testing } = useTestMonitor(monitor.id)
  const { disable, loading: disablingMaint } = useDisableMaintenanceMode()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const busy = pausing || resuming || deleting || testing || disablingMaint

  const act = async (fn: () => Promise<unknown>, okMsg: string) => {
    try {
      await fn()
      push(okMsg, 'success')
      onChanged()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Action failed', 'error')
    }
  }

  const inMaintenance = monitor.is_in_maintenance ?? false

  const actions: ActionItem[] = [
    {
      key: 'open',
      label: 'Open',
      icon: ExternalLink,
      onClick: () => navigate(`/monitors/${monitor.id}`),
    },
    {
      key: 'test',
      label: 'Test',
      icon: Play,
      disabled: busy,
      onClick: () => void act(() => test(), 'Test complete'),
    },
  ]

  if (access.canEdit) {
    actions.push(
      monitor.enabled
        ? {
            key: 'pause',
            label: 'Pause',
            icon: Pause,
            disabled: busy,
            onClick: () => void act(() => pause(), 'Monitor paused'),
          }
        : {
            key: 'resume',
            label: 'Resume',
            icon: Play,
            disabled: busy,
            onClick: () => void act(() => resume(), 'Monitor resumed'),
          },
    )
    if (inMaintenance) {
      actions.push({
        key: 'maint',
        label: 'End maintenance',
        icon: Wrench,
        disabled: busy,
        onClick: () => void act(() => disable(monitor.id), 'Maintenance ended'),
      })
    }
    actions.push({
      key: 'edit',
      label: 'Edit',
      icon: Pencil,
      onClick: () => navigate(`/monitors/${monitor.id}/edit`),
    })
  }

  if (access.canDelete) {
    actions.push({
      key: 'delete',
      label: 'Delete',
      icon: Trash2,
      danger: true,
      disabled: busy,
      onClick: () => setConfirmDelete(true),
    })
  }

  return (
    <>
      <ActionMenu items={actions} label={`Actions for ${monitor.name}`} />

      {confirmDelete && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onMouseDown={(e) => e.target === e.currentTarget && setConfirmDelete(false)}
        >
          <div
            role="dialog"
            aria-modal="true"
            className="w-full max-w-sm rounded-xl border border-white/10 bg-slate-900/95 p-6"
          >
            <h3 className="text-lg font-semibold text-white">Delete monitor?</h3>
            <p className="mt-2 text-sm text-slate-400">
              This permanently deletes {monitor.name} and all its history.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button
                className="btn-secondary"
                disabled={deleting}
                onClick={() => setConfirmDelete(false)}
              >
                Cancel
              </button>
              <button
                className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500 disabled:opacity-50"
                disabled={deleting}
                onClick={() =>
                  void act(async () => {
                    await del()
                    setConfirmDelete(false)
                  }, 'Monitor deleted')
                }
              >
                {deleting ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
