import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

// The API runs on :8080 in development (make run-api); the browser only ever
// talks to this origin, as it talks to portal.tenantry.site in the cluster,
// so the session cookies and redirects behave the same (IMPLEMENTATION-PLAN §10).
const api = { target: 'http://127.0.0.1:8080', changeOrigin: false, xfwd: true }

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { port: 5173, strictPort: true, proxy: { '/api': api, '/auth': api } },
  // Element Plus is one chunk (about 280 kB gzipped); fine for a portal behind a login.
  build: { chunkSizeWarningLimit: 1000 },
  test: { environment: 'jsdom', include: ['src/**/*.test.ts'] },
})
