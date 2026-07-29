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
        // Clinical cool-charcoal ramp (the "instrument enclosure"). Overriding
        // neutral re-tones every neutral-* utility across every page at once:
        // 900 = panel, 950 = deep chassis, 800 = hairline rules.
        neutral: {
          50: '#EDF2F4',
          100: '#DCE4E8',
          200: '#B9C6CE',
          300: '#90A2AD',
          400: '#6B7C87',
          500: '#4E5E68',
          600: '#3A4750',
          700: '#29343B',
          800: '#1B242B',
          900: '#111820',
          950: '#0A0E12',
        },
        // Unify every "alive / success" green (emerald-*, used across many
        // pages) onto the ECG phosphor ramp so there is exactly one green.
        emerald: {
          50: '#E6FFF0',
          100: '#BBFFD6',
          200: '#8DFABB',
          300: '#5CF49F',
          400: '#37F98A',
          500: '#22E07C',
          600: '#16B866',
          700: '#0F9552',
          800: '#0E7040',
          900: '#0C5632',
          950: '#052E1C',
        },
        warning: colors.amber,
        error: colors.red,
        info: colors.cyan,
        // Status colors: ECG green (alive), flatline red (down), dim (unknown).
        status: {
          online: '#37F98A',
          offline: '#FF4D4D',
          unknown: '#4E5E68',
        },
      },
      fontFamily: {
        // Vital Signs instrument type roles.
        sans: ['Inter Variable', 'Inter', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        display: ['Rajdhani', 'Inter Variable', 'system-ui', 'sans-serif'],
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
