// Platform health on the lab (IMPLEMENTATION-PLAN M6: the page comes with the
// cluster health check): the provisioning worker pings the cluster, so it
// shows Healthy, and the work in hand links to the queues.
import { expect, test } from '@playwright/test'
import { adminPassword, logIn } from './support'

test('a Platform Admin sees the platform healthy, the cluster checked', async ({ browser }) => {
  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/health')
  await expect(admin.getByRole('heading', { name: 'Platform health' })).toBeVisible()
  const row = (name: string) => admin.locator('.t-list li', { hasText: name })
  await expect(row('Platform API')).toContainText('Healthy')
  // Checked every 30 s by the worker: "last check" seconds ago, not "never".
  await expect(row('Cluster A')).toContainText('last check')
  await expect(row('Cluster A')).toContainText('Healthy')
  await expect(admin.getByText('Capacity on Cluster A')).toBeVisible()
  if (process.env.E2E_SHOTS)
    await admin.screenshot({ path: `${process.env.E2E_SHOTS}/platform-health.png`, fullPage: true })
})
