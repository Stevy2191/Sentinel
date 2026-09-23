/**
 * Reporting windows offered on the dashboard's headline card. Kept in its own
 * module so the constant does not live inside a component file that a page
 * would otherwise have to import purely for the type.
 */
export type ReportPeriod = '24h' | '7d' | '30d' | '90d'

export const REPORT_PERIODS: { key: ReportPeriod; label: string; heading: string; hours: number }[] = [
  { key: '24h', label: '24 hours', heading: 'Last 24 hours', hours: 24 },
  { key: '7d', label: '7 days', heading: 'Last 7 days', hours: 24 * 7 },
  { key: '30d', label: '30 days', heading: 'Last 30 days', hours: 24 * 30 },
  // 90 days is the furthest back this card can honestly reach: the summary it
  // draws is built from incidents, and incident retention defaults to 90 days,
  // so a longer window would trail off into history the purge has removed.
  { key: '90d', label: '90 days', heading: 'Last 90 days', hours: 24 * 90 },
]
