// M5's acceptance on the cluster (IMPLEMENTATION-PLAN M5, "Đầu ra kiểm chứng"):
// a Platform Admin creates an Organization with its owner and grant, takes
// fdp-k8s-wk3 out of the shared pool on the portal (only the database changes
// in M5) and gives it to the Organization, whose owner then sees the node and
// its remaining capacity; an ordinary user is refused /admin/*. The lab's
// database is put back as it was (wk3 in the pool again) even when a step
// fails.
import { expect, test, type Page } from '@playwright/test'
import { adminPassword, logIn, signUp } from './support'

const shots = process.env.E2E_SHOTS
async function shot(page: Page, name: string) {
  if (shots) await page.screenshot({ path: `${shots}/${name}.png`, fullPage: true })
}

const node = 'fdp-k8s-wk3'
const pool = 'shared-general'

// api calls the Platform API from a logged-in page, with the CSRF header the
// portal sends.
async function api(page: Page, method: string, path: string, body?: unknown) {
  return page.evaluate(
    async ([method, path, body]) => {
      const csrf = document.cookie
        .split('; ')
        .find((c) => c.startsWith('__Host-tenantry_csrf='))
        ?.split('=')[1]
      const res = await fetch('/api/v1' + path, {
        method,
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': decodeURIComponent(csrf ?? '') },
        body: body === undefined ? undefined : JSON.stringify(body),
      })
      return { status: res.status, body: res.status === 204 ? null : await res.json() }
    },
    [method, path, body] as const,
  )
}

// restore puts wk3 back in the shared pool, after taking it from whoever holds it.
async function restore(admin: Page, orgId: string | undefined) {
  if (orgId)
    await api(admin, 'PUT', `/admin/organizations/${orgId}/cluster-grants/cluster-a/dedicated-nodes`, { nodes: [] })
  const pools = await api(admin, 'GET', '/admin/clusters/cluster-a/shared-pools')
  const p = pools.body.items.find((x: { id: string }) => x.id === pool)
  if (!p.nodes.includes(node)) {
    const r = await api(admin, 'PUT', `/admin/clusters/cluster-a/shared-pools/${pool}`, {
      name: p.name,
      allocationOvercommitRatio: p.allocationOvercommitRatio,
      systemReserve: p.systemReserve,
      nodes: [...p.nodes, node].sort(),
    })
    expect(r.status, JSON.stringify(r.body)).toBe(200)
  }
}

test('an Admin creates an Organization and gives it a dedicated node', async ({ browser }) => {
  const owner = await signUp(browser) // the owner must have logged in once
  const slug = `e2eacme${Date.now().toString(36).slice(-6)}`
  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
  let orgId: string | undefined

  try {
    // Create the Organization, active at once, from the onboarding policy.
    await admin.getByRole('button', { name: 'Create Organization' }).click()
    const dialog = admin.getByRole('dialog', { name: 'Create Organization' })
    await dialog.locator('#co-name').fill(`E2E Acme ${slug}`)
    await dialog.locator('#co-slug').fill(slug)
    await dialog.locator('.el-select', { has: admin.locator('#co-owner') }).click()
    await admin.locator('#co-owner').fill(owner.who.email)
    await admin.getByRole('option', { name: owner.who.email, exact: true }).click()
    await expect(dialog.locator('#co-ws')).toHaveValue('2')
    await shot(admin, 'create-organization')
    await dialog.getByRole('button', { name: 'Create Organization' }).click()
    await expect(admin).toHaveURL(/\/admin\/organizations\/org-/)
    orgId = admin.url().split('/').pop()
    await expect(admin.getByRole('heading', { name: `E2E Acme ${slug}` })).toBeVisible()
    await expect(admin.getByText('Used now: 1')).toBeVisible() // the owner is its one member

    // Take wk3 out of the shared pool, then give it to the Organization.
    await admin.goto('/admin/clusters')
    const poolRow = admin.locator('.t-panel', { hasText: 'Shared pools' }).locator('.el-table__row', { hasText: pool })
    await poolRow.getByRole('button', { name: 'Change…' }).click()
    const poolDialog = admin.getByRole('dialog', { name: `Pool ${pool}` })
    await poolDialog.locator('li', { hasText: node }).locator('.el-checkbox').click()
    await expect(poolDialog.getByText(`Pods on ${node} keep running`)).toBeVisible()
    await shot(admin, 'pool-take-out-wk3')
    await poolDialog.getByRole('button', { name: 'Save pool' }).click()
    await expect(poolRow).not.toContainText(node)

    await admin.getByRole('button', { name: 'Give a node to an Organization' }).click()
    const give = admin.getByRole('dialog', { name: 'Give a node to an Organization' })
    await give.locator('.el-select', { has: admin.locator('#ng-node') }).click()
    await admin.getByRole('option', { name: new RegExp(`^${node} `) }).click()
    await give.locator('.el-select', { has: admin.locator('#ng-org') }).click()
    await admin.getByRole('option', { name: new RegExp(slug) }).click()
    await shot(admin, 'give-node')
    await give.getByRole('button', { name: 'Give node' }).click()
    const dedicatedRow = admin
      .locator('.t-panel', { hasText: 'Dedicated nodes' })
      .locator('.el-table__row', { hasText: node })
    await expect(dedicatedRow).toContainText(`E2E Acme ${slug}`)
    await shot(admin, 'clusters-after')

    // The owner sees the node and what is left on it: 3 CPU less 0.5 reserved.
    await owner.page.goto(`/orgs/${slug}/clusters`)
    const row = owner.page.locator('.el-table__row', { hasText: node })
    await expect(row).toContainText('2.5 of 2.5')
    await expect(row).toContainText('Not ready') // the operator checks the node from M8
    await expect(owner.page.getByRole('heading', { name: 'Shared pool' })).toBeVisible()
    await shot(owner.page, 'org-clusters')
    const options = await api(owner.page, 'GET', `/organizations/${slug}/clusters/cluster-a/compute-options`)
    expect(options.body.items).toEqual(
      expect.arrayContaining([expect.objectContaining({ mode: 'Dedicated', available: true })]),
    )

    // An ordinary user gets 403 from /admin/*.
    for (const [method, path] of [
      ['GET', '/admin/organizations'],
      ['GET', '/admin/clusters'],
      ['PUT', `/admin/organizations/${orgId}/entitlement`],
    ] as const) {
      const r = await api(
        owner.page,
        method,
        path,
        method === 'PUT' ? { maxMembers: 1, maxGroups: 3, maxWorkspacesTotal: 0 } : undefined,
      )
      expect(r.status, `${method} ${path}`).toBe(403)
    }
  } finally {
    await restore(admin, orgId)
  }

  // The lab is as before: wk3 back in the pool, dedicated to nobody.
  const pools = await api(admin, 'GET', '/admin/clusters/cluster-a/shared-pools')
  expect(pools.body.items.find((x: { id: string }) => x.id === pool).nodes).toContain(node)
  const nodes = await api(admin, 'GET', '/admin/clusters/cluster-a/nodes')
  expect(nodes.body.items.find((n: { name: string }) => n.name === node).dedicatedTo).toBeUndefined()
})
