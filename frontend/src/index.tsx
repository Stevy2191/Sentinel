import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Instrument typography, bundled locally so it works offline for self-hosters:
// Rajdhani = silkscreen device labels/headings, JetBrains Mono = digital
// readouts, Inter = prose. (See tailwind.config fontFamily.)
import '@fontsource/rajdhani/400.css'
import '@fontsource/rajdhani/500.css'
import '@fontsource/rajdhani/600.css'
import '@fontsource/rajdhani/700.css'
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

// Register the service worker for PWA/offline support. Production only, so it
// never interferes with the Vite dev server's HMR.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch((err) => {
      console.warn('Service worker registration failed:', err)
    })
  })
}
