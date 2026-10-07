import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Typography, bundled locally so it works offline for self-hosters: Inter for
// everything the reference sets in its single grotesque, JetBrains Mono for the
// few places that show raw values (cron strings, tokens, payloads).
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import App from '@/App'
import '@/index.css'
import { applyStoredPreferences } from '@/utils/preferences'

// Apply persisted visual preferences (font size, brand colors) before render.
applyStoredPreferences()

const container = document.getElementById('root')
if (!container) {
  throw new Error('Root element #root not found')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>
)

// No service worker is registered. The one Sentinel used to install served
// a cached copy of the app whenever a page load failed, which hid the
// browser's certificate warning after the self-signed certificate rotated
// and left sign-in failing with "Network error". public/sw.js now only
// removes that old worker from browsers that still have it.
