import colors from 'tailwindcss/colors'

// primary/accent are driven by CSS variables (RGB channel triplets) so the
// user's saved theme colors recolor every `primary-*`/`accent-*` utility at
// runtime. Defaults (emerald/amber) live in src/styles/theme.css.
const SHADES = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]
const cssVarScale = (name) =>
  Object.fromEntries(SHADES.map((s) => [s, `rgb(var(--${name}-${s}) / <alpha-value>)`]))

/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Sentinel design system semantic palette.
        primary: cssVarScale('primary'),
        accent: cssVarScale('accent'),
        warning: colors.amber,
        error: colors.red,
        info: colors.cyan,
        // Status colors, matching the reference's emerald/red/slate.
        status: {
          online: '#10b981',
          offline: '#ef4444',
          unknown: '#64748b',
        },
      },
      fontFamily: {
        // The reference sets everything in one grotesque; Inter is that face.
        // `display` is kept as an alias rather than removed so any lingering
        // font-display utility resolves to the same stack instead of falling
        // back to the browser default.
        sans: ['Inter Variable', 'Inter', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        display: ['Inter Variable', 'Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono Variable', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
      fontSize: {
        xs: ['0.75rem', { lineHeight: '1rem' }],
        sm: ['0.875rem', { lineHeight: '1.25rem' }],
        base: ['1rem', { lineHeight: '1.5rem' }],
        lg: ['1.125rem', { lineHeight: '1.75rem' }],
        xl: ['1.25rem', { lineHeight: '1.75rem' }],
        '2xl': ['1.5rem', { lineHeight: '2rem' }],
        '3xl': ['1.875rem', { lineHeight: '2.25rem' }],
      },
      borderRadius: {
        md: '6px',
        lg: '8px',
      },
      boxShadow: {
        card: '0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)',
        'card-hover': '0 4px 12px 0 rgb(0 0 0 / 0.12)',
      },
      transitionDuration: {
        DEFAULT: '150ms',
      },
    },
  },
  plugins: [],
}
