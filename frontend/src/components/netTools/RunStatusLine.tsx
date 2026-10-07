import { Loader2 } from 'lucide-react'
import type { ToolRun, ToolRunEvent } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { isFinalStatus, runStatusText, STATUS_TEXT } from '@/utils/netTools'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
  /** useRunStream's error: the browser gave up following the run. */
  streamError: string | null
}

/** "Waiting for file-server to pick up…", "Running — round 3 of 5",
 *  "Done — 0% loss, 2.1 ms avg", or why it failed. */
export default function RunStatusLine({ run, events, streamError }: Props) {
  const live = !isFinalStatus(run.status)
  return (
    <div className="space-y-1" aria-live="polite">
      <p className={`flex items-center gap-2 text-sm font-medium ${STATUS_TEXT[run.status]}`}>
        {live && <Loader2 className="h-4 w-4 shrink-0 animate-spin" aria-hidden />}
        {runStatusText(run, events)}
      </p>
      {live && streamError && (
        <p className={`text-xs ${colors.warning.text}`}>
          {streamError}. The run carries on; open it or reload to see how it ends.
        </p>
      )}
    </div>
  )
}
