// End-to-end tests against the portal on the lab cluster (IMPLEMENTATION-PLAN
// M3.8): the real Keycloak, the real API, mail through Mailpit. Run with
// `make e2e`, which supplies the admin password and the Mailpit port-forward.
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: 'e2e',
  // Each test signs people up and reviews requests on a shared cluster: one at a time.
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_PORTAL ?? 'https://portal.tenantry.site',
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
})
