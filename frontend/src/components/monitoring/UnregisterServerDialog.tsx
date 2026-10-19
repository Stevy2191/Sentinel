import type { Agent } from '@/hooks/useAgents'

interface Props {
  agent: Agent
  busy: boolean
  onCancel: () => void
  onConfirm: () => void
}

/** Confirms unregistering a server agent. */
export default function UnregisterServerDialog({ agent, busy, onCancel, onConfirm }: Props) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && onCancel()}
    >
      <div role="dialog" aria-modal="true" className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6">
        <h3 className="text-lg font-semibold text-white">Unregister {agent.name}?</h3>
        <p className="mt-2 text-sm text-slate-400">
          Its metric history is deleted with it. The agent on that host will keep running and start failing to report until
          it is stopped and removed.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button className="btn-secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500 disabled:opacity-50"
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? 'Removing…' : 'Unregister'}
          </button>
        </div>
      </div>
    </div>
  )
}
