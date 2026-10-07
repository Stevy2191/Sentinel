// The monitor the analytics view opens on: the one a link asked for (alert
// notifications link ?monitor_id=<id>) when it exists, otherwise the first.
// Returns '' while there are no monitors.
export function pickReportMonitor(ids: string[], requested: string | null): string {
  if (requested && ids.includes(requested)) return requested
  return ids[0] ?? ''
}
