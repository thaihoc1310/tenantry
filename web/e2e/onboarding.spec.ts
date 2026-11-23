// M3's acceptance on the cluster (IMPLEMENTATION-PLAN M3, "Đầu ra kiểm chứng"):
// a new person signs up and verifies their email, asks for an Organization,
// a Platform Admin approves it, and they see its quota; a reload keeps them
// logged in; logging out ends the Keycloak session too.
import { expect, test } from '@playwright/test'
import {
  adminPassword,
  cleanUpOrganization,
  cleanupAfterAll,
  logIn,
  openReview,
  orgIdOf,
  requestOrganization,
  signUp,
} from './support'

cleanupAfterAll()

test('a newcomer gets an approved Organization', async ({ browser }) => {
  const { page, who } = await signUp(browser)
  const slug = await requestOrganization(page, `E2E ${who.username}`)
  cleanUpOrganization(browser, await orgIdOf(page, slug), slug)

  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
  await openReview(admin, slug)
  await expect(admin.getByLabel('CPU requests')).toHaveValue('2')
  await admin.getByRole('button', { name: 'Approve with this grant' }).click()
  await expect(admin).toHaveURL(/tab=active/)
  await expect(admin.locator('.el-table__row', { hasText: slug })).toBeVisible()

  await page.getByRole('button', { name: 'Check again' }).click()
  await expect(page).toHaveURL(new RegExp(`/orgs/${slug}$`))
  await expect(page.getByText('Your Organization was approved')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Quota on cluster-a' })).toBeVisible()
  await expect(page.locator('.t-meter', { hasText: 'Shared CPU requests' })).toContainText('0 of 2 cores')
  await expect(page.locator('.t-kv')).toContainText('1 of 20')

  // A reload keeps the session.
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Quota on cluster-a' })).toBeVisible()

  // Logging out ends the SSO session: the next login asks for the password.
  await page.locator('.t-user').click()
  await page.getByText('Log out everywhere').click()
  await expect(page).toHaveURL(/\/login$/)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page.locator('#username')).toBeVisible()
})

test('a rejected requester sees why and may ask again', async ({ browser }) => {
  const { page, who } = await signUp(browser)
  const slug = await requestOrganization(page, `E2E reject ${who.username}`)
  cleanUpOrganization(browser, await orgIdOf(page, slug), slug)

  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
  await openReview(admin, slug)
  await admin.getByRole('button', { name: 'Reject…' }).click()
  await admin.fill('#rv-reason', 'Tell us how many environments you need.')
  await admin.getByRole('button', { name: 'Reject request' }).click()
  await expect(admin).toHaveURL(/tab=rejected/)

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Your request was not approved' })).toBeVisible()
  await expect(page.getByText('Tell us how many environments you need.')).toBeVisible()
  await page.getByRole('button', { name: 'Send a new request' }).click()
  // The slug was released: the same one can be asked for again.
  await page.fill('#org-name', `E2E reject ${who.username}`)
  await expect(page.locator('#org-slug')).toHaveValue(slug)
  await page.getByRole('button', { name: 'Send request' }).click()
  await expect(page.getByRole('heading', { name: 'Your request is waiting for review' })).toBeVisible()
  cleanUpOrganization(browser, await orgIdOf(page, slug), `${slug} (asked again)`)
})

test('a person without the role cannot open the admin area', async ({ browser }) => {
  const { page } = await signUp(browser)
  await page.goto('/admin/organizations')
  await expect(page).toHaveURL(/\/$/)
  const status = await page.evaluate(async () => (await fetch('/api/v1/admin/organizations')).status)
  expect(status).toBe(403)
})

// The access token lives 5 minutes; after that the API refreshes it at
// Keycloak. Slow, so only with E2E_SLOW=1.
test('the session outlives the access token', async ({ browser }) => {
  test.skip(!process.env.E2E_SLOW, 'set E2E_SLOW=1 to wait out an access token')
  test.setTimeout(8 * 60_000)
  const { page } = await signUp(browser)
  await expect(page.getByRole('heading', { name: 'Request an Organization' })).toBeVisible()
  await page.waitForTimeout(5 * 60_000 + 20_000)
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Request an Organization' })).toBeVisible()
  const status = await page.evaluate(async () => (await fetch('/api/v1/me')).status)
  expect(status).toBe(200)
})
