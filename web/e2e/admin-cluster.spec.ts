// DoD 1 (PRODUCT-PLAN §16.5): a Platform Admin registers a cluster from the
// portal. The record is a throwaway one (hosts under .invalid, never made
// Ready, so a grant on it is refused CLUSTER_NOT_READY) and goes again at
// the end through Delete cluster; cluster-a is not touched.
import { expect, test } from '@playwright/test'
import { adminPassword, api, cleanUp, cleanupAfterAll, logIn, onCleanup } from './support'

cleanupAfterAll()

test('an Admin registers a cluster, which stays Registered until made Ready, and deletes it', async ({ browser }) => {
  const id = `e2ereg${Date.now().toString(36).slice(-6)}`
  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/clusters')
  onCleanup(`cluster ${id}`, async () => {
    const r = await api(admin, 'DELETE', `/admin/clusters/${id}`)
    expect([204, 404], JSON.stringify(r.body)).toContain(r.status)
  })

  await admin.getByRole('button', { name: 'Register cluster' }).click()
  const dialog = admin.getByRole('dialog', { name: 'Register cluster' })
  await dialog.locator('#cl-id').fill(id)
  await dialog.locator('#cl-name').fill(`E2E ${id}`)
  await dialog.locator('#cl-proxy').fill(`https://kube-${id}.invalid`)
  await dialog.locator('#cl-console').fill(`https://console-${id}.invalid`)
  await dialog.locator('#cl-apps').fill(`${id}.invalid`)
  await dialog.locator('#cl-cidrs').fill('10.250.0.0/16')
  await dialog.getByRole('button', { name: 'Register cluster' }).click()
  await expect(dialog).toBeHidden()

  // Registered, with what was typed (the audit events are checked by the API tests).
  const got = await api(admin, 'GET', '/admin/clusters')
  const c = (got.body.items as { id: string; status: string; proxyEndpoint: string; reservedCidrs: string[] }[]).find(
    (x) => x.id === id,
  )
  expect(c?.status).toBe('Registered')
  expect(c?.proxyEndpoint).toBe(`https://kube-${id}.invalid`)
  expect(c?.reservedCidrs).toEqual(['10.250.0.0/16'])
  await admin.goto(`/admin/clusters?cluster=${id}`)
  await expect(admin.getByText(`https://kube-${id}.invalid`)).toBeVisible()
  await expect(admin.getByRole('heading', { name: 'Delete this cluster' })).toBeVisible()

  // The same id again is refused.
  const again = await api(
    admin,
    'POST',
    '/admin/clusters',
    {
      id,
      displayName: 'again',
      proxyEndpoint: `https://kube-${id}.invalid`,
      consoleUrl: `https://console-${id}.invalid`,
      appsDomain: `${id}.invalid`,
    },
    crypto.randomUUID(),
  )
  expect(again.status, JSON.stringify(again.body)).toBe(409)
  expect(again.body.details?.field).toBe('id')

  // Delete cluster, confirmed by its id; it is gone from the list.
  await admin.locator('#cl-del-confirm').fill(id)
  await admin.getByRole('button', { name: 'Delete cluster' }).click()
  await expect(admin.getByText(`is deleted`)).toBeVisible()
  const after = await api(admin, 'GET', '/admin/clusters')
  expect((after.body.items as { id: string }[]).some((x) => x.id === id)).toBe(false)
  await cleanUp()
})
