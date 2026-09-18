# Sentinel Frontend

React + TypeScript + Tailwind CSS admin UI and public status pages for
[Sentinel](../README.md), built with Vite.

## Tech Stack

- **React 18** + **TypeScript** (strict)
- **Vite** — dev server and build
- **Tailwind CSS** — styling (Sentinel design system)
- **React Router** — navigation
- **Axios** — API client
- **Recharts** — charts
- **lucide-react** — icons
- **date-fns** — date formatting
- **qrcode.react** — QR code for TOTP two-factor authentication setup

## Getting Started

```bash
# Install dependencies
npm install

# Copy env template (optional; the dev proxy works without it)
cp .env.example .env.local

# Start the dev server (http://localhost:3000)
npm run dev        # or: npm start

# Type-check and build for production
npm run build

# Preview the production build
npm run preview
```

The dev server proxies `/api` to the backend at `http://localhost:3001` (see
`vite.config.ts`), so the browser makes same-origin requests and no CORS
configuration is required. `/public/status/:slug` is not proxied — it's a SPA
route served by Vite itself, which then fetches its data through the proxied
`/api`. **Run the backend on port 3001** (e.g. `PORT=3001 ./bin/sentinel`) to
match, or change the proxy target.

## Environment Variables

Vite exposes only `VITE_*` and `REACT_APP_*` prefixed keys via `import.meta.env`.

| Variable | Default | Purpose |
| --- | --- | --- |
| `VITE_API_URL` | `/api/v1` (proxy) | API base URL (checked first) |
| `REACT_APP_API_URL` | `/api/v1` (proxy) | API base URL (fallback if `VITE_API_URL` is unset) |
| `REACT_APP_WS_URL` | `ws://localhost:3001` | WebSocket URL (declared, not yet used) |

## Project Structure

```
frontend/
├── index.html              # Vite entry HTML
├── vite.config.ts          # Vite config (aliases, dev proxy)
├── tailwind.config.js      # Sentinel design system
├── public/                 # Static assets (favicon, PWA manifest, service worker)
└── src/
    ├── index.tsx           # App entry point
    ├── App.tsx             # Router + providers
    ├── index.css           # Tailwind + global styles
    ├── styles/             # theme.css (CSS variables, dark mode)
    ├── components/         # Reusable components (Layout, modals, …)
    ├── pages/              # Route pages (Overview, Monitors, Reports, …)
    ├── context/            # Auth, Theme (light/dark/auto), and app config providers
    ├── services/           # api.ts (Axios instance + interceptors)
    ├── hooks/              # useMonitors and friends
    ├── types/              # Shared TypeScript types
    └── utils/              # Formatting, validation, and CSV export helpers
```

## Path Aliases

`@/*` resolves to `src/*` (configured in `tsconfig.json` and `vite.config.ts`).
