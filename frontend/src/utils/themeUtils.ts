// Theme color utilities: turn a single base hex color into a full Tailwind-style
// 50–950 shade ramp and apply it via CSS variables (RGB channel triplets), so
// every `primary-*` / `accent-*` utility class picks up the user's theme.

export const DEFAULT_PRIMARY = '#10b981'
export const DEFAULT_ACCENT = '#f59e0b'

const SHADES = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950] as const

// For each shade: mix the base color toward white (lighter) or black (darker).
const MIX: Record<number, { toward: 'white' | 'black'; ratio: number }> = {
  50: { toward: 'white', ratio: 0.92 },
  100: { toward: 'white', ratio: 0.84 },
  200: { toward: 'white', ratio: 0.68 },
  300: { toward: 'white', ratio: 0.48 },
  400: { toward: 'white', ratio: 0.24 },
  500: { toward: 'white', ratio: 0 },
  600: { toward: 'black', ratio: 0.12 },
  700: { toward: 'black', ratio: 0.28 },
  800: { toward: 'black', ratio: 0.42 },
  900: { toward: 'black', ratio: 0.55 },
  950: { toward: 'black', ratio: 0.7 },
}

type RGB = [number, number, number]

function hexToRgb(hex: string): RGB | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim())
  if (!m) return null
  const n = parseInt(m[1], 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function mix(base: RGB, toward: 'white' | 'black', ratio: number): RGB {
  const t = toward === 'white' ? 255 : 0
  return base.map((c) => Math.round(c * (1 - ratio) + t * ratio)) as RGB
}

/** generateScale returns { 50: "r g b", ..., 950: "r g b" } for a base hex. */
export function generateScale(hex: string): Record<number, string> {
  const base = hexToRgb(hex) ?? hexToRgb(DEFAULT_PRIMARY)!
  const out: Record<number, string> = {}
  for (const s of SHADES) {
    const [r, g, b] = mix(base, MIX[s].toward, MIX[s].ratio)
    out[s] = `${r} ${g} ${b}`
  }
  return out
}

// applyScale sets (or, for the default color, clears) the inline CSS variables
// for one scale. Clearing lets the exact default ramp in theme.css show through.
function applyScale(name: 'primary' | 'accent', hex: string, defaultHex: string) {
  const root = document.documentElement
  if (!hexToRgb(hex) || hex.toLowerCase() === defaultHex.toLowerCase()) {
    for (const s of SHADES) root.style.removeProperty(`--${name}-${s}`)
    return
  }
  const scale = generateScale(hex)
  for (const s of SHADES) root.style.setProperty(`--${name}-${s}`, scale[s])
}

/** applyThemeColors recolors the whole app from a primary + accent base hex. */
export function applyThemeColors(primary: string, accent: string): void {
  applyScale('primary', primary, DEFAULT_PRIMARY)
  applyScale('accent', accent, DEFAULT_ACCENT)
}
