// M8's acceptance on the cluster (IMPLEMENTATION-PLAN M8, "Đầu ra kiểm chứng"):
// fdp-k8s-wk3 leaves the shared pool and is given to an Organization
// (allocatable 3 CPU, reserve 0.5 → 2.5 CPU reservable); Water and Green
// reserve 1 CPU each, a third Workspace of 1 CPU is refused; their Pods run
// on wk3 only, and a Pod naming another node does not run. The lab is put
// back as it was (wk3 in the pool, dedicated to nobody) even when a step fails.
import { expect, test, type Page } from '@playwright/test'
import {
  api,
  deleteWorkspace,
  kubectl,
  kubectlIn,
  networkReady,
  succeeded,
  until,
  workspaceOrganization,
} from './support'

const node = 'fdp-k8s-wk3'
const reserve = { cpu: '500m', memory: '1536Mi', pods: 10 }

async function poolNodes(admin: Page, nodes: (all: string[]) => string[]) {
  const pools = await api(admin, 'GET', '/admin/clusters/cluster-a/shared-pools')
  const p = pools.body.items.find((x: { id: string }) => x.id === 'shared-general')
  const want = nodes(p.nodes).sort()
  if (JSON.stringify(want) === JSON.stringify([...p.nodes].sort())) return
  const r = await api(admin, 'PUT', '/admin/clusters/cluster-a/shared-pools/shared-general', {
    name: p.name,
    allocationOvercommitRatio: p.allocationOvercommitRatio,
    systemReserve: p.systemReserve,
    nodes: want,
  })
  expect(r.status, JSON.stringify(r.body)).toBe(200)
  if (r.body.operation) await succeeded(admin, r.body.operation.id)
}

const pod = (ns: string, name: string, nodeName?: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name, namespace: ns },
    spec: {
      ...(nodeName ? { nodeName } : {}),
      containers: [
        {
          name: 'pause',
          image: 'registry.k8s.io/pause:3.10',
          resources: { requests: { cpu: '100m', memory: '64Mi' }, limits: { cpu: '100m', memory: '64Mi' } },
        },
      ],
    },
  })

test('Dedicated Workspaces share a node without overbooking it', async ({ browser }) => {
  test.setTimeout(20 * 60_000)
  const { owner, admin, slug, admins } = await workspaceOrganization(browser, 'e2edn', 3)
  const page = owner.page
  const orgId = (await api(page, 'GET', `/organizations/${slug}`)).body.id
  const created: { id: string; slug: string }[] = []
  try {
    await poolNodes(admin, (all) => all.filter((n) => n !== node))
    const give = await api(admin, 'PUT', `/admin/organizations/${orgId}/cluster-grants/cluster-a/dedicated-nodes`, {
      nodes: [{ name: node, systemReserve: reserve }],
    })
    expect(give.status, JSON.stringify(give.body)).toBe(200)
    await succeeded(admin, give.body.operations[0].id)
    const grantId: string = give.body.items[0].id
    // The operator labels and taints wk3 for the Organization.
    expect(kubectl('get', 'node', node, '-o', 'jsonpath={.metadata.labels.tenantry\\.io/dedicated-org}')).toBe(orgId)
    expect(kubectl('get', 'node', node, '-o', 'jsonpath={.spec.taints[*].key}')).toContain('tenantry.io/dedicated-org')
    // The worker copies the node's state from the inventory.
    await until('wk3 Ready for the Organization', async () => {
      const r = await api(page, 'GET', '/organizations/' + slug + '/clusters/cluster-a/dedicated-nodes')
      return r.body.items?.[0]?.nodeState === 'Ready' ? true : undefined
    })

    const create = (ws: string, cpu: string) =>
      api(
        page,
        'POST',
        `/organizations/${slug}/workspaces`,
        {
          slug: ws,
          displayName: ws,
          clusterId: 'cluster-a',
          compute: { mode: 'Dedicated', dedicatedNodeGrantId: grantId },
          resources: { requestsCpu: cpu, requestsMemory: '4Gi', pods: 10, requestsStorage: '1Gi', namespaceQuota: 1 },
          namespaces: ['app'],
          bindings: [{ groupId: admins.id, role: 'TenantOwner' }],
        },
        crypto.randomUUID(),
      )
    for (const ws of ['water', 'green']) {
      const r = await create(ws, '1')
      expect(r.status, JSON.stringify(r.body)).toBe(202)
      created.push({ id: r.body.workspace.id, slug: ws })
      await succeeded(page, r.body.operation.id)
    }
    const third = await create('earth', '1')
    expect(third.status).toBe(409)
    expect(third.body.code).toBe('DEDICATED_NODE_INSUFFICIENT_CAPACITY')

    // The wizard says so on the Resources step and goes no further (M10).
    await page.goto(`/orgs/${slug}/workspaces/new`)
    await page.locator('#w-name').fill('Earth')
    await page.getByRole('button', { name: 'Continue' }).click()
    await page.getByRole('button', { name: /Dedicated node/ }).click()
    const card = page.getByRole('button', { name: new RegExp(node) })
    await expect(card).toHaveAttribute('aria-pressed', 'true')
    await expect(card).toContainText('0.5 of 2.5 cores left')
    await expect(card).toContainText('Also used by')
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByText('Same as requests on a dedicated node.').first()).toBeVisible()
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(
      page.getByText(`Only 0.5 cores left on node ${node}. (DEDICATED_NODE_INSUFFICIENT_CAPACITY)`),
    ).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Resources' })).toBeVisible()
    await page.screenshot({ path: test.info().outputPath('wizard-dedicated-full.png'), fullPage: true })
    await page.locator('.t-wstep', { hasText: 'Review' }).click()
    await expect(page.getByRole('heading', { name: 'Resources' })).toBeVisible()

    // Water's Pod lands on wk3; one naming wk2 does not run there.
    const ns = `${slug}-water-app`
    const asOwner = [
      `--as=oidc:${owner.who.username}`,
      '--as-group=oidc:tenantry:tenant-users',
      `--as-group=oidc:${admins.machineName}`,
      '--as-group=system:authenticated',
    ]
    await networkReady(ns)
    kubectlIn(pod(ns, 'here'), 'apply', '-f', '-', ...asOwner)
    const placed = await until('the Pod scheduled', async () => {
      const n = kubectl('-n', ns, 'get', 'pod', 'here', '-o', 'jsonpath={.spec.nodeName}').trim()
      return n || undefined
    })
    expect(placed).toBe(node)
    expect(kubectl('-n', ns, 'get', 'pod', 'here', '-o', 'jsonpath={.status.qosClass}')).toBe('Guaranteed')
    let elsewhere = ''
    try {
      kubectlIn(pod(ns, 'elsewhere', 'fdp-k8s-wk2'), 'apply', '-f', '-', ...asOwner)
      elsewhere = await until(
        'the Pod on wk2 refused',
        async () => {
          const phase = kubectl('-n', ns, 'get', 'pod', 'elsewhere', '-o', 'jsonpath={.status.phase}').trim()
          return phase && phase !== 'Pending' ? phase : undefined
        },
        60_000,
      )
    } catch (e) {
      elsewhere = 'refused at admission: ' + String((e as { stderr?: string }).stderr ?? e)
    }
    expect(elsewhere).not.toBe('Running')
  } finally {
    for (const w of created) await deleteWorkspace(page, w.id, w.slug, `${slug}-${w.slug}`)
    const back = await api(admin, 'PUT', `/admin/organizations/${orgId}/cluster-grants/cluster-a/dedicated-nodes`, {
      nodes: [],
    })
    for (const op of back.body?.operations ?? []) await succeeded(admin, op.id)
    await poolNodes(admin, (all) => [...new Set([...all, node])])
  }
  const nodes = await api(admin, 'GET', '/admin/clusters/cluster-a/nodes')
  expect(nodes.body.items.find((n: { name: string }) => n.name === node).dedicatedTo).toBeUndefined()
  expect(kubectl('get', 'node', node, '-o', 'jsonpath={.metadata.labels.tenantry\\.io/compute-pool}')).toBe(
    'shared-general',
  )
})
