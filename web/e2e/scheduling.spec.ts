// Pods the scheduler cannot place reach the portal (status.scheduling,
// known-limitations 9a): a Pod asking for more CPU than any node has, yet
// within the Workspace's quota, stays Pending; the Workspace page warns, the
// list shows a badge and the Namespaces tab a count, and the Workspace stays
// Ready throughout. Deleting the Pod clears them.
import { expect, test } from '@playwright/test'
import {
  api,
  cleanupAfterAll,
  kubectl,
  kubectlIn,
  sharedWorkspace,
  timed,
  until,
  workspaceOrganization,
} from './support'

cleanupAfterAll()

// 4 cores: the shared pool's nodes have 3 allocatable each.
const greedy = (ns: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name: 'greedy', namespace: ns },
    spec: {
      containers: [
        {
          name: 'greedy',
          image: 'docker.io/library/busybox:1.36.1',
          command: ['sleep', '3600'],
          resources: { requests: { cpu: '4', memory: '64Mi' }, limits: { cpu: '4', memory: '64Mi' } },
          securityContext: {
            allowPrivilegeEscalation: false,
            runAsNonRoot: true,
            runAsUser: 65534,
            capabilities: { drop: ['ALL'] },
            seccompProfile: { type: 'RuntimeDefault' },
          },
        },
      ],
    },
  })

test('a Pod the scheduler cannot place shows on the portal, and the Workspace stays Ready', async ({ browser }) => {
  test.setTimeout(15 * 60_000)
  const cpu = { requestsCpu: '5', limitsCpu: '5' }
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2esch', 1, 5, 5, cpu)
  const page = owner.page
  const ws = await sharedWorkspace(page, slug, 'big', [{ groupId: admins.id, role: 'TenantOwner' }], page, cpu)
  const workspace = async () => {
    const r = await api(page, 'GET', `/workspaces/${ws.id}`)
    expect(r.status, JSON.stringify(r.body)).toBe(200)
    expect(r.body.state, 'the Workspace stays Ready').toBe('Ready')
    return r.body
  }

  kubectlIn(greedy(ws.ns), 'apply', '-f', '-')
  const sc = await timed('Pod applied to status.scheduling on the API', () =>
    until('status.scheduling on the Workspace', async () => (await workspace()).scheduling),
  )
  expect(sc.unschedulablePods).toBe(1)
  expect(sc.message).toMatch(/Insufficient cpu/)
  expect(sc.since).toBeTruthy()

  await page.goto(`/orgs/${slug}/workspaces/big`)
  const banner = page.locator('[data-test="scheduling"]')
  await expect(banner).toContainText('1 Pod cannot be scheduled')
  await expect(banner).toContainText('Insufficient cpu')
  await expect(banner).toContainText('The shared pool has no node with room')
  await expect(banner).toContainText('Pool dùng chung')
  await page.screenshot({ path: test.info().outputPath('scheduling-banner.png'), fullPage: true })

  await page.goto(`/orgs/${slug}/workspaces/big?tab=namespaces`)
  await expect(page.getByRole('row', { name: new RegExp(ws.ns) }).locator('.el-tag', { hasText: /^1$/ })).toBeVisible()
  const ns = await api(page, 'GET', `/workspaces/${ws.id}/namespaces`)
  expect(ns.body.items.find((n: { name: string }) => n.name === ws.ns)?.unschedulablePods).toBe(1)

  await page.goto(`/orgs/${slug}/workspaces`)
  await expect(page.getByText('1 unschedulable')).toBeVisible()

  kubectl('-n', ws.ns, 'delete', 'pod', 'greedy', '--wait=true')
  await timed('Pod deleted to status.scheduling gone from the API', () =>
    until('status.scheduling cleared', async () => ((await workspace()).scheduling ? undefined : true)),
  )
  await page.goto(`/orgs/${slug}/workspaces/big`)
  await expect(page.locator('code', { hasText: `${slug}-big` })).toBeVisible() // loaded
  await expect(banner).toHaveCount(0)
  await page.goto(`/orgs/${slug}/workspaces`)
  await expect(page.getByText('unschedulable')).toHaveCount(0)
})
