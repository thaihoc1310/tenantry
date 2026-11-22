// M4's acceptance on the cluster (IMPLEMENTATION-PLAN M4, "Đầu ra kiểm chứng"):
// an owner invites a newcomer with a one-time link, the newcomer accepts it,
// the owner puts them in a new group, and once the group sync worker has
// mirrored it the newcomer's Keycloak token names the group; a refresh token
// picks up a group added later (architecture/06 §5). Also: the link works
// once and only for its address, removal ends portal access at once, and
// ownership changes hands only when the recipient accepts.
import { expect, test, type Browser, type Page } from '@playwright/test'
import {
  adminPassword,
  groupsOf,
  kubectlTokens,
  logIn,
  openReview,
  requestOrganization,
  signUp,
  token,
} from './support'

const shots = process.env.E2E_SHOTS // a directory: save screenshots of each screen, for review
async function shot(page: Page, name: string) {
  if (shots) await page.screenshot({ path: `${shots}/${name}.png`, fullPage: true })
}

// approvedOrganization signs up an owner and has the Platform Admin approve their request.
async function approvedOrganization(browser: Browser, label: string) {
  const owner = await signUp(browser)
  const slug = await requestOrganization(owner.page, `E2E ${label} ${owner.who.username}`)
  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
  await openReview(admin, slug)
  await admin.getByRole('button', { name: 'Approve with this grant' }).click()
  await expect(admin).toHaveURL(/tab=active/)
  await admin.context().close()
  await owner.page.goto(`/orgs/${slug}`)
  await expect(owner.page.getByRole('heading', { name: 'Quota on cluster-a' })).toBeVisible()
  return { ...owner, slug }
}

// choose opens the Element Plus select holding the input `input` (its own
// box covers the input) and picks the option.
async function choose(page: Page, input: string, option: string | RegExp) {
  await page.locator('.el-select', { has: page.locator(input) }).click()
  await page.getByRole('option', { name: option, exact: typeof option === 'string' }).click()
}

// invite fills the invitation dialog and returns the one-time link it shows.
async function invite(owner: Page, slug: string, email: string, role: 'OrgMember' | 'OrgAdmin', groups: string[] = []) {
  await owner.goto(`/orgs/${slug}/members`)
  await owner.getByRole('button', { name: 'Invite member' }).click()
  const dialog = owner.getByRole('dialog', { name: 'Invite a member' })
  await dialog.locator('#inv-email').fill(email)
  await dialog.getByRole('radio', { name: role }).check({ force: true })
  for (const g of groups) await choose(owner, '#inv-groups', g)
  if (groups.length) await dialog.getByText('Organization role').click() // a multiple select stays open
  await shot(owner, `members-invite-${role}`)
  await owner.getByRole('button', { name: 'Create invitation link' }).click()
  const link = await owner.locator('.t-copy > span').textContent()
  expect(link).toMatch(/\/invitations\/[A-Za-z0-9_-]{43}$/)
  await shot(owner, 'members-invite-link')
  await owner.getByRole('button', { name: 'Done' }).click()
  return link!
}

// The Keycloak name of a group, read from the API as the owner.
async function machineName(owner: Page, slug: string, group: string): Promise<string> {
  const groups = await owner.evaluate(
    async (s) =>
      (await (await fetch(`/api/v1/organizations/${s}/groups`)).json()) as {
        items: { name: string; machineName: string }[]
      },
    slug,
  )
  return groups.items.find((g) => g.name === group)!.machineName
}

async function waitSynced(owner: Page, group: string) {
  const row = owner.locator('.el-table__row', { hasText: group })
  await expect(row.getByText('Synced')).toBeVisible({ timeout: 60_000 })
}

test('an invited newcomer joins, gets a group, and their token names it', async ({ browser }) => {
  const { page: owner, slug } = await approvedOrganization(browser, 'people')
  await shot(owner, 'overview-owner')

  // The default groups reach Keycloak by themselves.
  await owner.goto(`/orgs/${slug}/groups`)
  for (const g of ['admins', 'developers', 'viewers']) await waitSynced(owner, g)
  await shot(owner, 'groups')

  const joe = await signUp(browser)
  const link = await invite(owner, slug, joe.who.email, 'OrgMember', ['developers'])
  await expect(owner.locator('.el-table__row', { hasText: joe.who.email })).toBeVisible()
  await shot(owner, 'members-with-invitation')

  // Before accepting, a token names no group of the Organization.
  const before = await kubectlTokens(joe.page)
  expect(groupsOf(before.id_token).filter((g) => g.startsWith('org_'))).toEqual([])

  // Someone else cannot use the link.
  const mallory = await signUp(browser)
  await mallory.page.goto(link)
  await expect(mallory.page.getByRole('heading', { name: 'This invitation is for another account' })).toBeVisible()
  await shot(mallory.page, 'invitation-mismatch')

  await joe.page.goto(link)
  await expect(joe.page.getByRole('heading', { name: /^Join E2E people/ })).toBeVisible()
  await expect(joe.page.getByText('Verified, matches')).toBeVisible()
  await shot(joe.page, 'invitation')
  await joe.page.getByRole('button', { name: 'Accept invitation' }).click()
  await expect(joe.page).toHaveURL(new RegExp(`/orgs/${slug}$`))
  await expect(joe.page.getByText('No Workspaces you can reach yet')).toBeVisible()
  await shot(joe.page, 'overview-member')

  // The link worked once.
  await joe.page.goto(link)
  await expect(joe.page.getByRole('heading', { name: 'This invitation link no longer works' })).toBeVisible()

  // A new group with Joe in it.
  await owner.goto(`/orgs/${slug}/groups`)
  await owner.getByRole('button', { name: 'Create group' }).click()
  await owner.locator('#grp-name').fill('backend')
  await owner.getByRole('dialog').getByRole('button', { name: 'Create group' }).click()
  await waitSynced(owner, 'backend')
  await owner.locator('.el-table__row', { hasText: 'backend' }).getByRole('button', { name: 'Members' }).click()
  const dialog = owner.getByRole('dialog', { name: 'Members of backend' })
  await choose(owner, '[aria-label="Member to add"]', new RegExp(joe.who.email))
  await dialog.getByRole('button', { name: 'Add' }).click()
  await expect(dialog.locator('.el-table__row', { hasText: joe.who.email }).getByText('Synced')).toBeVisible({
    timeout: 60_000,
  })
  await shot(owner, 'group-members')
  await dialog.getByRole('button', { name: 'Done' }).click()

  const backend = await machineName(owner, slug, 'backend')
  const developers = await machineName(owner, slug, 'developers')

  // A new login names both groups: developers from the invitation, backend.
  const fresh = await kubectlTokens(joe.page)
  expect(groupsOf(fresh.id_token)).toEqual(expect.arrayContaining([developers, backend]))

  // The refresh token from before Joe joined gets a token with the groups:
  // a refresh runs the group mapper again (architecture/06 §5).
  const refreshed = await token({
    grant_type: 'refresh_token',
    client_id: 'kubectl',
    refresh_token: before.refresh_token,
  })
  expect(groupsOf(refreshed.id_token)).toEqual(expect.arrayContaining([developers, backend]))

  // Removing Joe ends his portal access at once, and his groups follow in Keycloak.
  await owner.goto(`/orgs/${slug}/members`)
  await owner.locator('.el-table__row', { hasText: joe.who.email }).getByRole('button', { name: 'Remove' }).click()
  await owner.getByRole('button', { name: 'Remove member' }).click()
  await expect(owner.locator('.el-table__row', { hasText: joe.who.email })).toHaveCount(0)
  const status = await joe.page.evaluate(async (s) => (await fetch(`/api/v1/organizations/${s}`)).status, slug)
  expect(status).toBe(404)
  await expect
    .poll(async () => groupsOf((await kubectlTokens(joe.page)).id_token).filter((g) => g.startsWith('org_')), {
      timeout: 60_000,
    })
    .toEqual([])
})

test('ownership changes hands when the recipient accepts', async ({ browser }) => {
  const { page: owner, slug } = await approvedOrganization(browser, 'transfer')
  const erin = await signUp(browser)
  await erin.page.goto(await invite(owner, slug, erin.who.email, 'OrgAdmin'))
  await erin.page.getByRole('button', { name: 'Accept invitation' }).click()
  await expect(erin.page).toHaveURL(new RegExp(`/orgs/${slug}$`))

  await owner.goto(`/orgs/${slug}/settings`)
  await choose(owner, '#set-owner', new RegExp(erin.who.email))
  await owner.getByRole('button', { name: 'Start transfer' }).click()
  await expect(owner.getByText(`Waiting for ${erin.who.first} ${erin.who.last} to accept`)).toBeVisible()
  await shot(owner, 'settings-owner-waiting')

  // The recipient sees the offer on the Overview and accepts it in Settings.
  await erin.page.reload()
  await expect(erin.page.getByText(/wants to hand .* over to you/)).toBeVisible()
  await shot(erin.page, 'overview-transfer-banner')
  await erin.page.getByRole('button', { name: 'Review' }).click()
  await expect(erin.page.getByRole('button', { name: 'Accept ownership' })).toBeDisabled()
  await erin.page.getByText('I understand that I become responsible for this Organization.').click()
  await shot(erin.page, 'settings-recipient')
  await erin.page.getByRole('button', { name: 'Accept ownership' }).click()
  await expect(erin.page.getByRole('heading', { name: 'Transfer ownership' })).toBeVisible()

  // The old owner is an OrgAdmin now, and may leave.
  await owner.goto(`/orgs/${slug}`)
  await expect(owner.getByText('You are OrgAdmin')).toBeVisible()
  await owner.goto(`/orgs/${slug}/settings`)
  await owner.getByRole('button', { name: /^Leave / }).click()
  await owner
    .getByRole('dialog')
    .getByRole('button', { name: /^Leave / })
    .click()
  await expect(owner).toHaveURL(/\/$/)
})
