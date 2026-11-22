// What the end-to-end tests share: new accounts, Mailpit, logging in.
import { expect, type Browser, type Page } from '@playwright/test'

export const mailpit = process.env.E2E_MAILPIT ?? 'http://127.0.0.1:8025'

export function adminPassword(): string {
  const p = process.env.E2E_ADMIN_PASSWORD
  if (!p) throw new Error('E2E_ADMIN_PASSWORD is not set (make e2e reads it from the cluster)')
  return p
}

// A person who does not exist yet: unique per run, recognisable as a test.
export function newcomer() {
  const id = `e2e${Date.now().toString(36)}${Math.floor(Math.random() * 1e4).toString(36)}`
  return { username: id, email: `${id}@tenantry.site`, password: `Pw-${crypto.randomUUID()}`, first: 'E2E', last: id }
}

// verificationLink waits for Keycloak's mail to email and returns its link.
export async function verificationLink(email: string): Promise<string> {
  for (let i = 0; i < 30; i++) {
    try {
      const res = await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${email}"`)}`)
      const { messages } = (await res.json()) as { messages: { ID: string }[] }
      if (messages.length) {
        const msg = (await (await fetch(`${mailpit}/api/v1/message/${messages[0]!.ID}`)).json()) as { Text: string }
        const link = /https:\/\/\S+\/login-actions\/action-token\?\S+/.exec(msg.Text)?.[0]
        if (link) return link
      }
    } catch {
      // The port-forward to Mailpit drops a connection now and then: ask again.
    }
    await new Promise((r) => setTimeout(r, 1000))
  }
  throw new Error(`no verification mail for ${email} in Mailpit`)
}

// signUp registers a new account through Keycloak's own pages: the form asks
// no password while the realm verifies email (Keycloak 26), the link in the
// mail does, so only the owner of the address can set one. It ends on the
// portal, logged in.
export async function signUp(browser: Browser) {
  const who = newcomer()
  const page = await (await browser.newContext()).newPage()
  await page.goto('/')
  await page.getByRole('link', { name: /register/i }).click()
  await page.fill('#username', who.username)
  await page.fill('#email', who.email)
  await page.fill('#firstName', who.first)
  await page.fill('#lastName', who.last)
  await page.getByRole('button', { name: /register/i }).click()
  // Not logged in to the portal before the address is verified.
  await expect(page.getByText(/verify your email/i).first()).toBeVisible()
  await page.goto(await verificationLink(who.email))
  await page.fill('#password-new', who.password)
  await page.fill('#password-confirm', who.password)
  await page.getByRole('button', { name: /submit/i }).click()
  await page.waitForURL((u) => !u.pathname.startsWith('/realms/'))
  return { page, who }
}

// logIn opens a new browser context and logs in at Keycloak.
export async function logIn(browser: Browser, username: string, password: string, path = '/'): Promise<Page> {
  const page = await (await browser.newContext()).newPage()
  await page.goto(path)
  await page.fill('#username', username)
  await page.fill('#password', password)
  await page.click('#kc-login')
  await page.waitForURL((u) => !u.pathname.startsWith('/realms/'))
  return page
}

// requestOrganization fills the request form; it returns the slug.
export async function requestOrganization(page: Page, name: string): Promise<string> {
  await expect(page.getByRole('heading', { name: 'Request an Organization' })).toBeVisible()
  await page.fill('#org-name', name)
  await page.fill('#org-purpose', 'End-to-end test of the onboarding flow.')
  const slug = await page.inputValue('#org-slug')
  await page.getByRole('button', { name: 'Send request' }).click()
  await expect(page.getByRole('heading', { name: 'Your request is waiting for review' })).toBeVisible()
  return slug
}

// openReview opens the review page of the request with this slug.
export async function openReview(admin: Page, slug: string) {
  await admin.goto('/admin/organizations')
  const row = admin.locator('.el-table__row', { hasText: slug })
  await row.getByRole('button', { name: 'Review' }).click()
  await expect(admin.getByText('Starting grant on cluster-a')).toBeVisible()
}
