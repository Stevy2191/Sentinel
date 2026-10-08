import { useState } from 'react'
import { Pencil } from 'lucide-react'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  notes: string | null
  canEdit: boolean
  busy: boolean
  onSave: (notes: string) => Promise<unknown>
}

/** A site's free-form notes, edited in place. Line breaks are kept. */
export default function NotesCard({ notes, canEdit, busy, onSave }: Props) {
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const save = async () => {
    if (draft === null) return
    setError(null)
    try {
      await onSave(draft)
      setDraft(null)
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the notes')
    }
  }

  return (
    <section className="card space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-lg font-light text-white">Notes</h2>
        {canEdit && draft === null && (
          <button className="btn-secondary flex items-center gap-1.5 !py-1 text-sm" onClick={() => setDraft(notes ?? '')}>
            <Pencil className="h-3.5 w-3.5" /> Edit
          </button>
        )}
      </div>
      {draft !== null ? (
        <div className="space-y-2">
          <textarea
            className={inputCls}
            rows={6}
            maxLength={10000}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            aria-label="Site notes"
            autoFocus
          />
          {error && <p className="text-sm text-red-400">{error}</p>}
          <div className="flex justify-end gap-2">
            <button
              className="btn-secondary"
              disabled={busy}
              onClick={() => {
                setDraft(null)
                setError(null)
              }}
            >
              Cancel
            </button>
            <button className="btn-primary" disabled={busy} onClick={() => void save()}>
              {busy ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
      ) : notes ? (
        <p className="whitespace-pre-wrap break-words text-sm text-slate-300">{notes}</p>
      ) : (
        <p className="text-sm text-slate-500">{canEdit ? 'No notes yet.' : 'No notes recorded.'}</p>
      )}
    </section>
  )
}
