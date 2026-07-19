import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
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
