import { useState } from 'react'
import { Loader2, MessageSquare, Trash2, Pencil, Check, X } from 'lucide-react'
import { useIncidentComments, type IncidentComment } from '@/hooks/useIncidents'
import { useAuthContext } from '@/context/AuthContext'
import { formatDatetime } from '@/utils/formatters'

/**
 * The comment thread on an incident.
 *
 * Separate from the root cause and resolution fields above it, which are the
 * summary: one statement each, overwritten on every save. An investigation is
 * not one statement — observations arrive over time and from different people,
 * and "what did we know at 03:00" is exactly what a postmortem asks. A single
 * field cannot answer that, because each edit destroys the last answer.
 */
export default function IncidentThread({
  incidentId,
  comments,
  onChanged,
}: {
  incidentId: string
  comments: IncidentComment[]
  onChanged: () => void
}) {
  const { add, update, remove, busy } = useIncidentComments(incidentId)
  const { currentUser } = useAuthContext()
  const [draft, setDraft] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState('')
  const [error, setError] = useState<string | null>(null)

  const canModify = (c: IncidentComment) =>
    !!currentUser && (currentUser.is_admin || c.user_id === currentUser.user_id)

  const run = async (fn: () => Promise<void>) => {
    setError(null)
    try {
      await fn()
      onChanged()
    } catch (err) {
      setError((err as { message?: string }).message ?? 'Could not save the comment')
    }
  }

  const submit = async () => {
    const body = draft.trim()
    if (!body) return
    await run(async () => {
      await add(body)
      setDraft('')
    })
  }

  const saveEdit = async (id: string) => {
    const body = editDraft.trim()
    if (!body) return
    await run(async () => {
      await update(id, body)
      setEditingId(null)
    })
  }

  return (
    <div>
      <h3 className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
        <MessageSquare className="h-3.5 w-3.5" aria-hidden />
        Notes &amp; discussion
        {comments.length > 0 && <span className="text-slate-500">({comments.length})</span>}
      </h3>

      {comments.length === 0 ? (
        <p className="mb-3 text-sm text-slate-500">
          Nothing recorded yet. Add what you found, what you changed, or why this was not a real
          outage.
        </p>
      ) : (
        <ul className="mb-3 space-y-2">
          {comments.map((c) => (
            <li key={c.id} className="rounded-lg border border-white/10 bg-slate-800/40 p-3">
              <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2">
                <span className="text-sm font-medium text-white">{c.author_name}</span>
                <span className="text-xs text-slate-500">
                  {formatDatetime(c.created_at)}
                  {/* Flagged so a rewritten comment is not mistaken for what
                      was said at the time. */}
                  {c.updated_at !== c.created_at && ' · edited'}
                </span>
              </div>

              {editingId === c.id ? (
                <div className="space-y-2">
                  <textarea
                    value={editDraft}
                    onChange={(e) => setEditDraft(e.target.value)}
                    rows={3}
                    className="w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white"
                  />
                  <div className="flex justify-end gap-2">
                    <button
                      className="btn-secondary !py-1"
                      onClick={() => setEditingId(null)}
                      disabled={busy}
                    >
                      <X className="h-3.5 w-3.5" /> Cancel
                    </button>
                    <button
                      className="btn-primary !py-1"
                      onClick={() => void saveEdit(c.id)}
                      disabled={busy || !editDraft.trim()}
                    >
                      <Check className="h-3.5 w-3.5" /> Save
                    </button>
                  </div>
                </div>
              ) : (
                <>
                  <p className="whitespace-pre-wrap text-sm text-slate-300">{c.body}</p>
                  {canModify(c) && (
                    <div className="mt-2 flex justify-end gap-1">
                      <button
                        className="rounded p-1 text-slate-500 transition hover:text-white"
                        aria-label="Edit this comment"
                        onClick={() => {
                          setEditingId(c.id)
                          setEditDraft(c.body)
                        }}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                      <button
                        className="rounded p-1 text-slate-500 transition hover:text-red-400"
                        aria-label="Delete this comment"
                        disabled={busy}
                        onClick={() => void run(() => remove(c.id))}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  )}
                </>
              )}
            </li>
          ))}
        </ul>
      )}

      <textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        rows={3}
        placeholder="Add a note — what you saw, what you changed, what to watch for."
        className="w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500"
      />
      {error && <p className="mt-1 text-xs text-red-400">{error}</p>}
      <div className="mt-2 flex justify-end">
        <button className="btn-primary !py-1.5" onClick={() => void submit()} disabled={busy || !draft.trim()}>
          {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <MessageSquare className="h-4 w-4" />}
          Add note
        </button>
      </div>
    </div>
  )
}
