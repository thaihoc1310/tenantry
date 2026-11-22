// M10's acceptance for the Create Workspace wizard (IMPLEMENTATION-PLAN M10,
// "Test bắt buộc"): an OrgOwner creates a Shared Workspace through the seven
// steps and watches it reach Ready. On the way the server's refusals land on
// their step (a slug taken, CPU beyond the grant), a group not synced yet
// cannot be chosen, and
// each state is kept as a screenshot. The Workspaces are deleted at the end.
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { api, deleteWorkspace, kubectl, succeeded, until, workspaceOrganization } from './support'

const shot = (page: Page, info: TestInfo, name: string) =>
  page.screenshot({ path: info.outputPath(`${name}.png`), fullPage: true })

async function setNumber(page: Page, id: string, value: string) {
  const input = page.locator(`#${id}`)
  await input.fill(value)
  await input.blur()
}

test('the wizard creates a Shared Workspace that reaches Ready', async ({ browser }, info) => {
  test.setTimeout(15 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2ewz')
  const page = owner.page
  const made: { id: string; slug: string }[] = []
  try {
    // A Workspace made through the API takes the slug "taken".
    const first = await api(
      page,
      'POST',
      `/organizations/${slug}/workspaces`,
      {
        slug: 'taken',
        displayName: 'Taken',
        clusterId: 'cluster-a',
        compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
        resources: {
          requestsCpu: '250m',
          requestsMemory: '512Mi',
          limitsCpu: '500m',
          limitsMemory: '1Gi',
          pods: 5,
          requestsStorage: '1Gi',
          namespaceQuota: 1,
        },
        bindings: [{ groupId: admins.id, role: 'TenantOwner' }],
      },
      crypto.randomUUID(),
    )
    expect(first.status, JSON.stringify(first.body)).toBe(202)
    made.push({ id: first.body.workspace.id, slug: 'taken' })
    await succeeded(page, first.body.operation.id)

    // Keycloak syncs a new group in seconds, too fast to catch on screen: the
    // page gets its groups with viewers still Pending.
    await page.route(`**/api/v1/organizations/${slug}/groups`, async (route) => {
      const r = await route.fetch()
      const body = await r.json()
      body.items = body.items.map((g: { name: string }) => (g.name === 'viewers' ? { ...g, syncStatus: 'Pending' } : g))
      await route.fulfill({ response: r, json: body })
    })
    await page.goto(`/orgs/${slug}/workspaces`)
    await page.getByRole('button', { name: 'Create Workspace' }).click()
    await expect(page.getByRole('heading', { name: 'Create Workspace' })).toBeVisible()

    // 1 Name: nothing yet is refused before the server is asked; a taken slug by the server.
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByText('Give the Workspace a name.')).toBeVisible()
    await page.locator('#w-name').fill('Taken')
    await expect(page.locator('#w-slug')).toHaveValue('taken')
    await expect(page.getByText(`${slug}-taken`).first()).toBeVisible()
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByText('(SLUG_TAKEN)')).toBeVisible()
    await shot(page, info, '1-slug-taken')
    await page.locator('#w-name').fill('Solar')
    await page.locator('#w-slug').fill('solar')
    await page.getByRole('button', { name: 'Continue' }).click()

    // 2 Cluster and compute: the only cluster and pool are chosen.
    await expect(page.getByRole('heading', { name: 'Cluster and compute' })).toBeVisible()
    await expect(page.getByText('Chosen for you: it is the only cluster granted')).toBeVisible()
    await expect(page.getByRole('button', { name: /Shared nodes/ })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByText('shared-general').first()).toBeVisible()
    await shot(page, info, '2-compute')
    await page.getByRole('button', { name: 'Continue' }).click()

    // 3 Resources: more CPU than the grant has left (2 cores, 0.25 held).
    await expect(page.getByRole('heading', { name: 'Resources' })).toBeVisible()
    await expect(page.getByText('1.75 left in the shared quota').first()).toBeVisible()
    await setNumber(page, 'w-rcpu', '3')
    await setNumber(page, 'w-lcpu', '3')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(
      page.getByText(/Only 1\.75 cores left in the Organization's grant.*CLUSTER_GRANT_QUOTA_EXCEEDED/),
    ).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Resources' })).toBeVisible()
    await shot(page, info, '3-over-quota')
    // A limit under its request is refused on the page.
    await setNumber(page, 'w-rcpu', '0.5')
    await setNumber(page, 'w-lcpu', '0.25')
    await expect(page.getByText('A limit cannot be lower than its request.')).toBeVisible()
    await setNumber(page, 'w-lcpu', '1')
    await setNumber(page, 'w-rmem', '1')
    await setNumber(page, 'w-lmem', '2')
    await setNumber(page, 'w-sto', '2')
    await page.getByRole('button', { name: 'Continue' }).click()

    // 4 Network: Auto, with the block it would get now.
    await expect(page.getByRole('heading', { name: 'Network' })).toBeVisible()
    await expect(page.getByText('onboarding-pod-network')).toBeVisible()
    await expect(page.getByText('if created now')).toBeVisible()
    await shot(page, info, '4-network')
    await page.getByRole('button', { name: 'Continue' }).click()

    // 5 Namespaces: one created with the Workspace.
    await expect(page.getByRole('heading', { name: 'Namespaces' })).toBeVisible()
    await page.locator('.el-select', { has: page.locator('#w-ns') }).click()
    await page.locator('#w-ns').pressSequentially('web')
    await page.locator('#w-ns').press('Enter')
    await page.keyboard.press('Escape')
    await expect(page.getByText(`${slug}-solar-web`)).toBeVisible()
    await page.getByRole('button', { name: 'Continue' }).click()

    // 6 Access: admins are the owners; viewers, not synced, cannot be chosen.
    await expect(page.getByRole('heading', { name: 'Access' })).toBeVisible()
    await expect(page.locator('.el-select', { has: page.locator('#w-own') })).toContainText('admins')
    await expect(page.locator('.el-select', { has: page.locator('#w-dev') })).toContainText('developers')
    await expect(page.getByText('Groups still being synced cannot be chosen yet')).toBeVisible()
    await page.locator('.el-select', { has: page.locator('#w-view') }).click()
    await expect(page.getByRole('option', { name: 'viewers (sync pending)' })).toHaveClass(/is-disabled/)
    await shot(page, info, '6-group-pending')
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Continue' }).click()

    // 7 Review, then Create: the Workspace page follows the create to Ready.
    await expect(page.getByRole('heading', { name: 'Review' })).toBeVisible()
    await expect(page.getByText('Fix the steps marked with !')).toHaveCount(0)
    await expect(page.getByText('Shared pool shared-general')).toBeVisible()
    await shot(page, info, '7-review')
    await page.getByRole('button', { name: 'Create Workspace' }).click()
    await page.waitForURL(`**/orgs/${slug}/workspaces/solar`)
    const ws = await until('the Workspace in the list', async () => {
      const r = await api(page, 'GET', `/organizations/${slug}/workspaces`)
      return r.body.items?.find((w: { slug: string }) => w.slug === 'solar')
    })
    made.push({ id: ws.id, slug: 'solar' })
    await shot(page, info, '8-provisioning')
    await expect(page.locator('.t-head').getByText('Ready', { exact: true })).toBeVisible({ timeout: 5 * 60_000 })
    await shot(page, info, '9-ready')
    expect(ws.resources).toMatchObject({
      requestsCpu: '500m',
      limitsCpu: '1',
      requestsMemory: '1Gi',
      limitsMemory: '2Gi',
    })
    expect(ws.network.networkPoolId).toBe('onboarding-pod-network')
    kubectl('get', 'namespace', `${slug}-solar-web`)
  } finally {
    for (const w of made) await deleteWorkspace(page, w.id, w.slug, `${slug}-${w.slug}`)
  }
})
