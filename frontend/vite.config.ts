import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  // Expose REACT_APP_* (in addition to VITE_*) env vars via import.meta.env.
  envPrefix: ['VITE_', 'REACT_APP_'],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 3000,
    // Proxy API calls to the backend so the browser sees same-origin requests
    // (avoids CORS, which the backend does not currently enable).
    proxy: {
      // Carries the public status page JSON at /api/v1/public/status/:slug too.
      // /public/status/:slug is NOT proxied — it is the SPA route, served by Vite.
      '/api': {
        target: 'http://localhost:3001',
        changeOrigin: true,
      },
    },
  },
})
