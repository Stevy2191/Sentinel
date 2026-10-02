// Pure helpers for the MIB library and browser pages (no React, no network).

/** Whether a string is a plain dotted-numeric OID, e.g. "1.3.6.1.2.1.1". Mirrors
 *  the backend's own test-walk validation (testwalk_handler.go's oidPattern). */
export function isValidOid(value: string): boolean {
  return /^\d+(\.\d+)+$/.test(value.trim())
}

/** "1 normal · 2 warning · 3 critical", sorted by the numeric value rather
 *  than however the object's keys happened to arrive. */
export function formatEnum(enumMap: Record<string, string> | null | undefined): string {
  if (!enumMap) return ''
  return Object.entries(enumMap)
    .sort(([a], [b]) => Number(a) - Number(b))
    .map(([value, name]) => `${value} ${name}`)
    .join(' · ')
}
