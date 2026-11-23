// ADR-0022 on the lab: "Log out everywhere" on the portal ends the Keycloak
// SSO session, so the person is out everywhere that session reached: the
// portal at once, kubectl (a kubelogin-style login gets no code without a
// password), and the web console, which asks for a login again. Until now
// this was checked against a fake Keycloak only (TestLogout).
import { expect, test } from '@playwright/test'
import {
  api,
  cleanUp,
  cleanupAfterAll,
  kubectlAuthorize,
  logOutOfPortal,
  openConsole,
  sharedWorkspace,
  timed,
  until,
  workspaceOrganization,
} from './support'

cleanupAfterAll()

test('logging out of the portal ends the Keycloak session: kubectl and Headlamp ask for a login again', async ({
  browser,
}) => {
  test.setTimeout(20 * 60_000)
  const { owner, admin, slug, admins } = await workspaceOrganization(browser, 'e2elo')
  const page = owner.page
  // The owner logs out during the test: the Platform Admin removes the Workspace.
  const solar = await sharedWorkspace(page, slug, 'solar', [{ groupId: admins.id, role: 'TenantOwner' }], admin)

  // Before: the SSO session answers Headlamp and kubectl without a password.
  const hl = await timed('Headlamp sign-in with the portal session', () => openConsole(page, solar.id))
  await expect(hl.getByText(solar.ns).first()).toBeVisible({ timeout: 60_000 })
  const consoleUrl = hl.url()
  expect(await kubectlAuthorize(page), 'a kubectl code from the SSO session').not.toBe('login')

  await page.bringToFront()
  await timed('portal logout', () => logOutOfPortal(page))
  const loggedOut = Date.now()

  // The portal: no session.
  expect((await api(page, 'GET', '/me')).status).toBe(401)
  // kubectl: Keycloak shows its login form, no code comes back.
  expect(await timed('kubectl login after logout', () => kubectlAuthorize(page))).toBe('login')

  // Headlamp, opened again from where the person was: it may serve the token
  // it holds until that expires (5 minutes), then it asks to sign in, and
  // signing in shows Keycloak's form instead of coming straight back.
  await hl.bringToFront()
  const asked = await until(
    'Headlamp asking for a login',
    async () => {
      await hl.goto(consoleUrl)
      const signIn = hl.getByRole('button', { name: /^sign in$/i })
      const form = hl.locator('#kc-form-login')
      await Promise.race([
        signIn.waitFor({ timeout: 15_000 }),
        form.waitFor({ timeout: 15_000 }),
        hl.getByText(solar.ns).first().waitFor({ timeout: 15_000 }),
      ]).catch(() => {})
      if (await form.isVisible()) return Date.now()
      if (await signIn.isVisible()) {
        await signIn.click()
        await expect(form, 'Keycloak asks for a login').toBeVisible({ timeout: 30_000 })
        return Date.now()
      }
      return undefined
    },
    6 * 60_000,
  )
  const took = `Headlamp asked for a login ${((asked - loggedOut) / 1000).toFixed(0)} s after the portal logout`
  test.info().annotations.push({ type: 'duration', description: took })
  console.log(took)

  await cleanUp()
})
