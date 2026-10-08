/** A one-line empty state with an optional action. */
export default function EmptyLine({ text, action, onAction }: { text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border border-white/10 bg-slate-800/40 px-4 py-3 text-sm text-slate-400">
      <span>{text}</span>
      {action && onAction && (
        <button className="text-primary-400 hover:underline" onClick={onAction}>
          {action}
        </button>
      )}
    </div>
  )
}
