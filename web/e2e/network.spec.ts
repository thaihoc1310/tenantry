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

    // Deleting Water quarantines its block: the Organization still sees it, taken.
    await deleteWorkspace(page, water!.id, 'water', `${slug}-water`)
    made.pop()
    const a = await api(page, 'GET', `/organizations/${slug}/network-pools/onboarding-pod-network/availability`)
    expect(a.body.blocks).toEqual(
      expect.arrayContaining([expect.objectContaining({ cidr: water!.cidr, state: 'Quarantined' })]),
    )
  } finally {
    for (const w of made) await deleteWorkspace(page, w.id, w.slug, `${slug}-${w.slug}`)
  }
})
