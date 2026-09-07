import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

/**
 * Flat config (ESLint 9). Deliberately not type-aware: the type-checked rule
 * sets need a full program per run, which is slow, and `npm run typecheck`
 * already covers what they would catch. This config is for the things the
 * compiler cannot see - hook dependency arrays above all, which is why the
 * codebase already carries react-hooks/exhaustive-deps disable comments that
 * nothing was enforcing.
 */
export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**', 'public/**', 'coverage/**'] },

  js.configs.recommended,
  ...tseslint.configs.recommended,

  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.es2022 },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,

      // A missing dependency is a stale-closure bug waiting to happen, so it is
      // an error rather than a warning. The existing disable comments stay
      // where a dep is intentionally omitted.
      'react-hooks/exhaustive-deps': 'error',

      // Fast Refresh only works when a module exports components alone. Warn
      // rather than error: several pages legitimately export a helper beside
      // the component, and breaking the build over HMR ergonomics is wrong.
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],

      // Unused values are usually a leftover from an edit. Underscore-prefixed
      // names are the escape hatch for deliberately-ignored args.
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrors: 'none' },
      ],

      // `any` defeats the point of the typecheck step. Warn rather than error
      // so it shows up without blocking, since the codebase is currently clean
      // and this is here to keep it that way.
      '@typescript-eslint/no-explicit-any': 'warn',

      // Leftover debugging should not reach main. console.warn/error are fine.
      'no-console': ['error', { allow: ['warn', 'error'] }],
      'no-debugger': 'error',
    },
  },

  // Config files run in Node, not the browser.
  {
    files: ['*.config.{js,ts}', 'vite.config.ts', 'tailwind.config.js', 'postcss.config.js'],
    languageOptions: { globals: { ...globals.node } },
  }
)
