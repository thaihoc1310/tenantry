// M10's acceptance for the Create Workspace wizard (IMPLEMENTATION-PLAN M10,
// "Test bắt buộc"): an OrgOwner creates a Shared Workspace through the seven
// steps and watches it reach Ready. On the way the server's refusals land on
// their step (a slug taken, CPU beyond the grant), a group not synced yet
// cannot be chosen, and
// each state is kept as a screenshot. The Workspaces are deleted at the end.
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import {
  api,
  cleanUp,
  cleanupAfterAll,
  deleteWorkspace,
  kubectl,
  onCleanup,
  succeeded,
  until,
  workspaceOrganization,
} from './support'

const shot = (page: Page, info: TestInfo, name: string) =>
  page.screenshot({ path: info.outputPath(`${name}.png`), fullPage: true })

async function setNumber(page: Page, id: string, value: string) {
  const input = page.locator(`#${id}`)
  await input.fill(value)
  await input.blur()
}

cleanupAfterAll()

test('the wizard creates a Shared Workspace that reaches Ready', async ({ browser }, info) => {
  test.setTimeout(15 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2ewz')
  const page = owner.page
  const made: { id: string; slug: string }[] = []
  onCleanup('the Workspaces of the wizard test', async () => {
    for (const w of made) await deleteWorkspace(page, w.id, w.slug, `${slug}-${w.slug}`)
  })
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

  // Namespaces tab: one more from the portal, then deleted (no volumes, so no name to type).
  await page.getByRole('link', { name: 'Namespaces' }).click()
  await expect(page.getByText('1 of 3')).toBeVisible()
  await page.getByRole('button', { name: 'Create namespace' }).click()
  const dialog = page.getByRole('dialog', { name: 'Create namespace' })
  await dialog.locator('#ns-name').fill('api')
  await dialog.getByRole('button', { name: 'Create namespace' }).click()
  const apiRow = page.getByRole('row', { name: new RegExp(`${slug}-solar-api`) })
  await expect(apiRow).toContainText('Active', { timeout: 3 * 60_000 })
  kubectl('get', 'namespace', `${slug}-solar-api`)
  await shot(page, info, '10-namespaces')
  await expect(page.getByRole('button', { name: 'Create namespace' })).toBeEnabled({ timeout: 60_000 })
  await page.getByRole('button', { name: `Delete ${slug}-solar-api` }).click()
  await page
    .getByRole('dialog', { name: `Delete ${slug}-solar-api` })
    .getByRole('button', { name: 'Delete namespace' })
    .click()
  await expect(apiRow).toHaveCount(0, { timeout: 3 * 60_000 })
  await until('the namespace gone from the cluster', async () =>
    kubectl('get', 'namespace', `${slug}-solar-api`, '--ignore-not-found') === '' ? true : undefined,
  )

  // Access roles tab: developers become Viewers; the last owner group cannot be removed.
  await page.getByRole('link', { name: 'Access roles' }).click()
  await expect(page.getByRole('row', { name: /admins/ }).getByRole('button', { name: 'Remove' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Add group' })).toBeEnabled({ timeout: 60_000 })
  await page.getByRole('combobox', { name: 'Role of group developers' }).click()
  await page.getByRole('option', { name: 'Viewer', exact: true }).click()
  await expect(page.getByText('developers is Viewer now.')).toBeVisible()
  await until('the binding changed', async () => {
    const r = await api(page, 'GET', `/workspaces/${ws.id}/bindings`)
    return r.body.items?.find(
      (b: { groupName: string; role: string }) => b.groupName === 'developers' && b.role === 'Viewer',
    )
  })
  await shot(page, info, '11-access-roles')

  // The Organization's audit trail has the create and the changes.
  await page.getByRole('link', { name: 'Audit' }).click()
  for (const a of ['workspace.create', 'namespace.create', 'namespace.delete', 'workspace_binding.update'])
    await expect(page.getByText(a, { exact: true }).first()).toBeVisible()
  await shot(page, info, '12-audit')
  await cleanUp()
})

// The states of PRODUCT-PLAN §19.2 the wizard shows, each kept as a
// screenshot. Bringing them about for real would change the shared lab (a
// cluster in Maintenance, a pool with no block left, a node not Ready), so the
// answers the page gets are rewritten; the server's own refusals are covered
// by the API tests and by the test above.
test('the wizard explains what stops a Workspace', async ({ browser }, info) => {
  test.setTimeout(5 * 60_000)
  const { owner, slug } = await workspaceOrganization(browser, 'e2ews9')
  const page = owner.page
  const rewrite = (path: string, f: (body: { items: object[] }) => unknown) =>
    page.route(`**/api/v1${path}`, async (route) => {
      const r = await route.fetch()
      await route.fulfill({ response: r, json: f(await r.json()) })
    })
  const refuse = (code: string, details: Record<string, unknown>) =>
    page.route(`**/api/v1/organizations/${slug}/workspaces:validate`, (route) =>
      route.fulfill({ status: 409, json: { code, message: code, requestId: 'e2e', details } }),
    )

  // The cluster in Maintenance.
  await rewrite(`/organizations/${slug}/clusters`, (b) => ({
    items: b.items.map((c: object) => ({ ...c, status: 'Maintenance' })),
  }))
  await page.goto(`/orgs/${slug}/workspaces/new`)
  await page.locator('#w-name').fill('Moon')
  await page.locator('.t-wstep', { hasText: 'Cluster and compute' }).click()
  await expect(page.getByText('is in maintenance')).toBeVisible()
  await expect(page.getByText('No Ready cluster is granted to the Organization.')).toBeVisible()
  await shot(page, info, 's1-cluster-maintenance')
  await page.unrouteAll()

  // No dedicated node Ready.
  await rewrite(`/organizations/${slug}/clusters/cluster-a/compute-options`, () => ({
    items: [
      { mode: 'Shared', available: true },
      { mode: 'Dedicated', available: true },
    ],
  }))
  await rewrite(`/organizations/${slug}/clusters/cluster-a/dedicated-nodes`, () => ({
    items: [
      {
        id: 'ng-e2e',
        name: 'fdp-k8s-wk9',
        nodeState: 'NotReady',
        capacityState: 'Available',
        reservable: { cpu: '2', memory: '4Gi', pods: 50 },
        reserved: { cpu: '0', memory: '0', pods: 0 },
        remaining: { cpu: '2', memory: '4Gi', pods: 50 },
        workspaces: [],
      },
    ],
  }))
  await page.goto(`/orgs/${slug}/workspaces/new`)
  await page.locator('#w-name').fill('Moon')
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.getByRole('button', { name: /Dedicated node/ }).click()
  await expect(page.getByRole('button', { name: /fdp-k8s-wk9/ })).toBeDisabled()
  await expect(page.getByText('The node is not ready. Ask a Platform Admin.')).toBeVisible()
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByText('Choose a node.')).toBeVisible()
  await shot(page, info, 's2-no-node')
  await page.unrouteAll()

  // The network pool has no block left.
  await refuse('NETWORK_POOL_EXHAUSTED', { networkPoolId: 'onboarding-pod-network' })
  await page.goto(`/orgs/${slug}/workspaces/new`)
  await page.locator('#w-name').fill('Moon')
  await page.locator('.t-wstep', { hasText: 'Review' }).click()
  await expect(page.getByRole('heading', { name: 'Network' })).toBeVisible()
  await expect(page.getByText('(NETWORK_POOL_EXHAUSTED)')).toBeVisible()
  await shot(page, info, 's3-pool-exhausted')
  await page.unrouteAll()

  // The Organization has as many Workspaces as it may.
  await refuse('ENTITLEMENT_EXCEEDED', { limit: 'maxWorkspacesTotal' })
  await page.goto(`/orgs/${slug}/workspaces/new`)
  await page.locator('#w-name').fill('Moon')
  await page.locator('.t-wstep', { hasText: 'Review' }).click()
  await expect(page.getByText('(ENTITLEMENT_EXCEEDED)').first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Create Workspace' })).toBeDisabled()
  await shot(page, info, 's4-org-full')
})
