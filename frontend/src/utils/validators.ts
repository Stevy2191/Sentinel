// Client-side input validators mirroring the backend's rules.

/** validateUrl accepts http(s) URLs and bare host:port targets (for tcp/dns). */
export function validateUrl(url: string): boolean {
  const trimmed = url.trim()
  if (!trimmed) return false
  try {
    // Full URL with scheme.
    // eslint-disable-next-line no-new
    new URL(trimmed)
    return true
  } catch {
    // Fall back to host or host:port (e.g. "example.com:443", "localhost").
    return /^[a-zA-Z0-9.-]+(:\d{1,5})?$/.test(trimmed)
  }
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export function validateEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim())
}

export function validatePort(port: number): boolean {
  return Number.isInteger(port) && port >= 1 && port <= 65535
}

/** Monitor name: non-empty after trimming, at most 255 chars. */
export function validateMonitorName(name: string): boolean {
  const trimmed = name.trim()
  return trimmed.length >= 1 && trimmed.length <= 255
}

const SLUG_RE = /^[a-zA-Z0-9-]+$/

/** Status page slug: 3-50 chars, alphanumeric and hyphens only. */
export function validateStatusPageSlug(slug: string): boolean {
  const trimmed = slug.trim()
  return trimmed.length >= 3 && trimmed.length <= 50 && SLUG_RE.test(trimmed)
}
