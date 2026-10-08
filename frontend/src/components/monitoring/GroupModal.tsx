import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import ColorPicker from '@/components/ColorPicker'
import { useCreateMonitorGroup, useDeleteMonitorGroup, useUpdateMonitorGroup } from '@/hooks/useMonitorGroups'
import type { MonitorGroup } from '@/types'

// A group's default colour, stored with the group (not page styling).
const DEFAULT_GROUP_COLOR = '#10b981'

export default function GroupModal({
  mode,
  group,
  onClose,
  onSaved,
  push,
}: {
  mode: 'create' | 'edit'
  group?: MonitorGroup
  onClose: () => void
  onSaved: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const { create, loading: creating } = useCreateMonitorGroup()
  const { update, loading: updating } = useUpdateMonitorGroup()
  const { delete: remove, loading: deleting } = useDeleteMonitorGroup()
  const [name, setName] = useState(group?.name ?? '')
  const [description, setDescription] = useState(group?.description ?? '')
  const [color, setColor] = useState(group?.color ?? DEFAULT_GROUP_COLOR)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const busy = creating || updating || deleting
  const inputCls =
    'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

  const save = async () => {
    if (!name.trim()) {
      push('Group name is required', 'error')
      return
    }
    const input = { name: name.trim(), description: description.trim() || null, color }
    try {
      if (mode === 'create') {
        await create(input)
        push('Group created', 'success')
      } else if (group) {
        await update(group.id, input)
        push('Group updated', 'success')
      }
      onSaved()
      onClose()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to save group', 'error')
    }
  }
  const del = async () => {
    if (!group) return
    try {
      await remove(group.id)
      push('Group deleted; its monitors were ungrouped', 'success')
      onSaved()
      onClose()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to delete group', 'error')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <h3 className="text-lg font-semibold">{mode === 'create' ? 'Create Group' : 'Edit Group'}</h3>
        <div>
          <span className="mb-1 block text-sm font-medium">
            Name <span className="text-red-500">*</span>
          </span>
          <input autoFocus className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="Internal Servers" />
        </div>
        <div>
          <span className="mb-1 block text-sm font-medium">Description</span>
          <input className={inputCls} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
        </div>
        <ColorPicker label="Color" value={color} defaultValue={DEFAULT_GROUP_COLOR} onChange={setColor} />
        <div className="flex items-center justify-between gap-2 border-t border-white/10 pt-4">
          {mode === 'edit' ? (
            <button
              className="btn border border-red-500/30 text-red-400 hover:bg-red-500/10"
              disabled={busy}
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-4 w-4" /> Delete
            </button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <button className="btn-secondary" disabled={busy} onClick={onClose}>
              Cancel
            </button>
            <button className="btn-primary" disabled={busy} onClick={() => void save()}>
              {busy ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
        {confirmDelete && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
            <p className="mb-2 text-amber-300">Delete this group? Its monitors will be ungrouped (not deleted).</p>
            <div className="flex justify-end gap-2">
              <button className="btn-secondary !py-1" onClick={() => setConfirmDelete(false)}>
                Cancel
              </button>
              <button className="btn bg-error-600 !py-1 text-white hover:bg-error-700" disabled={deleting} onClick={() => void del()}>
                {deleting ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
