// M9's acceptance on the cluster (IMPLEMENTATION-PLAN M9, "Đầu ra kiểm chứng"):
// Solar and Water get different /24 blocks of onboarding-pod-network; their
// Pods' addresses are in them; a Pod of Solar reaches another Namespace of
// Solar but not Water; deleting Water quarantines its block. (Released
// after the 10-minute quarantine is covered by the worker's tests.)
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

const agnhost = 'registry.k8s.io/e2e-test-images/agnhost:2.53'
const pod = (ns: string, name: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name, namespace: ns, labels: { app: name } },
    spec: {
      containers: [
        {
          name: 'agnhost',
          image: agnhost,
          args: ['netexec', '--http-port=8080'],
          resources: { requests: { cpu: '50m', memory: '64Mi' }, limits: { cpu: '50m', memory: '64Mi' } },
        },
      ],
    },
  })

const inBlock = (ip: string, cidr: string) => {
  const [net, bits] = cidr.split('/')
  const n = (a: string) => a.split('.').reduce((x, o) => x * 256 + Number(o), 0)
  const size = 2 ** (32 - Number(bits))
  return Math.floor(n(ip) / size) === Math.floor(n(net!) / size)
}

test('Workspaces get their own address blocks and cannot reach each other', async ({ browser }) => {
  test.setTimeout(15 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2enw')
  const page: Page = owner.page
  const made: { id: string; slug: string; cidr: string }[] = []
  try {
    for (const [ws, namespaces] of [
      ['solar', ['app', 'web']],
      ['water', ['app']],
    ] as const) {
      const r = await api(
        page,
        'POST',
        `/organizations/${slug}/workspaces`,
        {
          slug: ws,
          displayName: ws,
          clusterId: 'cluster-a',
          compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
          resources: {
            requestsCpu: '200m',
            requestsMemory: '256Mi',
            limitsCpu: '200m',
            limitsMemory: '256Mi',
            pods: 4,
            requestsStorage: '1Gi',
            namespaceQuota: 2,
          },
          namespaces,
          bindings: [{ groupId: admins.id, role: 'TenantOwner' }],
        },
        crypto.randomUUID(),
      )
      expect(r.status, JSON.stringify(r.body)).toBe(202)
      made.push({ id: r.body.workspace.id, slug: ws, cidr: r.body.workspace.network.cidr })
      await succeeded(page, r.body.operation.id)
    }
    const [solar, water] = made
    expect(solar!.cidr).not.toBe(water!.cidr)
    // The cluster's IPPool of each Workspace has its block from the database.
    for (const w of made) {
      const cidr = await until(
        `IPPool ${slug}-${w.slug}`,
        async () =>
          kubectl(
            'get',
            'ippools.crd.projectcalico.org',
            `${slug}-${w.slug}`,
            '--ignore-not-found',
            '-o',
            'jsonpath={.spec.cidr}',
          ).trim() || undefined,
      )
      expect(cidr).toBe(w.cidr)
    }

    const ip: Record<string, string> = {}
    for (const ns of [`${slug}-solar-app`, `${slug}-solar-web`, `${slug}-water-app`]) {
      await networkReady(ns)
      kubectlIn(pod(ns, 'srv'), 'apply', '-f', '-')
      kubectl('-n', ns, 'wait', '--for=condition=Ready', 'pod/srv', '--timeout=180s')
      ip[ns] = kubectl('-n', ns, 'get', 'pod', 'srv', '-o', 'jsonpath={.status.podIP}').trim()
    }
    expect(inBlock(ip[`${slug}-solar-app`]!, solar!.cidr), `${ip[`${slug}-solar-app`]} in ${solar!.cidr}`).toBe(true)
    expect(inBlock(ip[`${slug}-water-app`]!, water!.cidr), `${ip[`${slug}-water-app`]} in ${water!.cidr}`).toBe(true)

    const connect = (to: string) => {
      try {
        kubectl('-n', `${slug}-solar-app`, 'exec', 'srv', '--', '/agnhost', 'connect', `${to}:8080`, '--timeout=3s')
        return true
      } catch {
        return false
      }
    }
    expect(
      await until('Solar to Solar', async () => (connect(ip[`${slug}-solar-web`]!) ? true : undefined), 60_000),
    ).toBe(true)
    expect(connect(ip[`${slug}-water-app`]!)).toBe(false)

    // Deleting Water: its IPPool and IPAM blocks go before the block is
    // quarantined; the Organization still sees it, taken, and the name too.
    await deleteWorkspace(page, water!.id, 'water', `${slug}-water`)
    made.pop()
    expect(kubectl('get', 'ippools.crd.projectcalico.org', `${slug}-water`, '--ignore-not-found')).toBe('')
    const blocks = kubectl('get', 'ipamblocks.crd.projectcalico.org', '-o', 'jsonpath={.items[*].spec.cidr}').split(' ')
    expect(blocks.some((b) => b && inBlock(b.split('/')[0]!, water!.cidr))).toBe(false)
    const again = await api(
      page,
      'POST',
      `/organizations/${slug}/workspaces`,
      {
        slug: 'water',
        displayName: 'water',
        clusterId: 'cluster-a',
        compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
        resources: {
          requestsCpu: '100m',
          requestsMemory: '128Mi',
          limitsCpu: '100m',
          limitsMemory: '128Mi',
          pods: 1,
          requestsStorage: '1Gi',
          namespaceQuota: 1,
        },
        bindings: [{ groupId: admins.id, role: 'TenantOwner' }],
      },
      crypto.randomUUID(),
    )
    expect(again.status, JSON.stringify(again.body)).toBe(409)
    expect(again.body.code).toBe('SLUG_TAKEN')
    const a = await api(page, 'GET', `/organizations/${slug}/network-pools/onboarding-pod-network/availability`)
    expect(a.body.blocks).toEqual(
      expect.arrayContaining([expect.objectContaining({ cidr: water!.cidr, state: 'Quarantined' })]),
    )
  } finally {
    for (const w of made) await deleteWorkspace(page, w.id, w.slug, `${slug}-${w.slug}`)
  }
})
