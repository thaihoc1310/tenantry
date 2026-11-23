// End-to-end tests against the portal on the lab cluster (IMPLEMENTATION-PLAN
// M3.8): the real Keycloak, the real API, mail through Mailpit. Run with
// `make e2e`, which supplies the admin password and the Mailpit port-forward.
import { defineConfig, devices } from '@playwright/test'
import { traceRecording } from './e2e/trace'

export default defineConfig({
  testDir: 'e2e',
  // Each test signs people up and reviews requests on a shared cluster: one at a time.
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_PORTAL ?? 'https://portal.tenantry.site',
    // A click on something that never shows fails in 30 s instead of
    // hanging until the test's timeout, so the cleanup still has time.
    actionTimeout: 30_000,
    viewport: { width: 1440, height: 900 },
    // No DOM snapshots: they would keep the passwords typed (e2e/trace.ts).
    trace: { mode: 'retain-on-failure', ...traceRecording },
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
})
