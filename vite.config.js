import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Pango ships as one Go binary + one SQLite file (docs/ARCHITECTURE.md §1).
// In development the API lives on its own port (`./backend/pango --demo
// --addr 127.0.0.1:8799`) and Vite proxies /api/* to it so the browser sees
// one origin and the session cookie behaves exactly as it will in production.
const apiTarget = process.env.PANGO_API_PROXY || 'http://127.0.0.1:8799'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
  build: {
    target: 'es2022',
    outDir: 'dist',
    sourcemap: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.js'],
    css: true,
    // e2e/ holds Playwright specs (npm run test:e2e), not Vitest ones — both
    // use the *.spec.js suffix, so without this Vitest tries to collect them
    // too and fails on the `@playwright/test` import.
    exclude: ['**/node_modules/**', '**/dist/**', 'e2e/**'],
  },
})
