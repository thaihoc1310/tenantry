// M11's access with real OIDC tokens (IMPLEMENTATION-PLAN M11, "Đầu ra kiểm
// chứng"): through Capsule Proxy at kube-a, a Developer sees the Namespaces of
// their own Workspace only, deploys there, reads no Node, cannot write an
// Ingress or create a Namespace; a Viewer only reads; neither reaches a
// Workspace their groups have no role in. The web console (Headlamp) signs in
// with the portal's Keycloak session, also for someone in 20 groups, whose
// session cookie is the largest. Taking someone out of a group keeps the
// revocation contract (architecture/06 §5): the portal at once, Kubernetes
// when the token already issued expires, within 5 minutes.
import { execFileSync } from 'node:child_process'
import { expect, test, type Page } from '@playwright/test'
import {
  api,
  cleanUp,
  cleanupAfterAll,
  deleteWorkspace,
  groupsOf,
  kubectlTokens,
  member,
  networkReady,
  onCleanup,
  succeeded,
  until,
  workspaceOrganization,
} from './support'

cleanupAfterAll()

const proxy = process.env.E2E_KUBE_PROXY ?? 'https://kube-a.tenantry.site'

// kubectlAs runs kubectl against the proxy with nothing but a person's token;
// it returns what kubectl printed, or its refusal.
function kubectlAs(token: string, input: string, ...args: string[]): { ok: boolean; out: string } {
  try {
    const out = execFileSync('kubectl', ['--kubeconfig=/dev/null', `--server=${proxy}`, `--token=${token}`, ...args], {
      input,
      encoding: 'utf8',
      stdio: ['pipe', 'pipe', 'pipe'],
    })
    return { ok: true, out }
  } catch (e) {
    return { ok: false, out: String((e as { stderr?: string }).stderr ?? e) }
  }
}

const pod = (ns: string, name: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name, namespace: ns },
    spec: {
      containers: [
        {
          name: 'pause',
          image: 'registry.k8s.io/pause:3.10',
          resources: { requests: { cpu: '50m', memory: '32Mi' }, limits: { cpu: '50m', memory: '32Mi' } },
        },
      ],
    },
  })

async function workspace(page: Page, slug: string, ws: string, bindings: { groupId: string; role: string }[]) {
  const res = await api(
    page,
    'POST',
    `/organizations/${slug}/workspaces`,
    {
      slug: ws,
      displayName: ws,
      clusterId: 'cluster-a',
      compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
      resources: {
        requestsCpu: '250m',
        requestsMemory: '256Mi',
        limitsCpu: '500m',
        limitsMemory: '512Mi',
        pods: 5,
        requestsStorage: '1Gi',
        namespaceQuota: 1,
      },
      namespaces: ['app'],
      bindings,
    },
    crypto.randomUUID(),
  )
  expect(res.status, JSON.stringify(res.body)).toBe(202)
  const id: string = res.body.workspace.id
  onCleanup(`Workspace ${ws}`, () => deleteWorkspace(page, id, ws, `${slug}-${ws}`))
  await succeeded(page, res.body.operation.id)
  return { id, ns: `${slug}-${ws}-app` }
}

// openConsole opens the Workspace's web console from the portal's link and
// signs in to Headlamp with the browser's Keycloak session.
async function openConsole(page: Page, wsId: string) {
  const link = await api(page, 'GET', `/workspaces/${wsId}/console-link`)
  expect(link.status, JSON.stringify(link.body)).toBe(200)
  const consolePage = await page.context().newPage()
  await consolePage.goto(link.body.url)
  // Headlamp asks once to sign in; Keycloak answers at once, the session is the portal's.
  const signIn = consolePage.getByRole('button', { name: /^sign in$/i })
  await signIn.waitFor({ timeout: 30_000 })
  await signIn.click()
  await consolePage.waitForURL((u) => u.host === new URL(link.body.url).host, { timeout: 60_000 })
  return consolePage
}

test('a Developer and a Viewer reach their own Workspace only, through the proxy and the console', async ({
  browser,
}) => {
  test.setTimeout(20 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2eac')
  const page = owner.page
  const groups = (await api(page, 'GET', `/organizations/${slug}/groups`)).body.items as {
    id: string
    name: string
    syncStatus: string
  }[]
  const developers = groups.find((g) => g.name === 'developers')!
  const viewers = groups.find((g) => g.name === 'viewers')!
  await until('viewers and developers in Keycloak', async () => {
    const r = await api(page, 'GET', `/organizations/${slug}/groups`)
    return (r.body.items as { name: string; syncStatus: string }[]).every((g) => g.syncStatus === 'Synced')
      ? true
      : undefined
  })
  const solar = await workspace(page, slug, 'solar', [
    { groupId: admins.id, role: 'TenantOwner' },
    { groupId: developers.id, role: 'Developer' },
    { groupId: viewers.id, role: 'Viewer' },
  ])
  const wind = await workspace(page, slug, 'wind', [{ groupId: admins.id, role: 'TenantOwner' }])
  await networkReady(solar.ns)

  const joe = await member(browser, page, slug, [developers])
  const bob = await member(browser, page, slug, [viewers])
  const joeToken = (await kubectlTokens(joe.page)).id_token
  const bobToken = (await kubectlTokens(bob.page)).id_token

  // Joe, a Developer: his Workspace's Namespace only, Pods there, no Node.
  const nss = kubectlAs(joeToken, '', 'get', 'namespaces', '-o', 'name')
  expect(nss.ok, nss.out).toBe(true)
  expect(nss.out).toContain(`namespace/${solar.ns}`)
  expect(nss.out).not.toContain(wind.ns)
  const created = kubectlAs(joeToken, pod(solar.ns, 'joe'), 'apply', '-f', '-')
  expect(created.ok, created.out).toBe(true)
  expect(kubectlAs(joeToken, '', '-n', solar.ns, 'get', 'pods', '-o', 'name').out).toContain('pod/joe')
  const nodes = kubectlAs(joeToken, '', 'get', 'nodes', '-o', 'name')
  expect(nodes.ok ? nodes.out.trim() : 'forbidden').toMatch(/^$|forbidden/)
  expect(kubectlAs(joeToken, '', 'create', 'namespace', `${slug}-solar-joe`).ok).toBe(false)
  const ingress = JSON.stringify({
    apiVersion: 'networking.k8s.io/v1',
    kind: 'Ingress',
    metadata: { name: 'joe', namespace: solar.ns },
    spec: {
      rules: [
        {
          host: `joe-${slug}.example.com`,
          http: {
            paths: [{ path: '/', pathType: 'Prefix', backend: { service: { name: 'x', port: { number: 80 } } } }],
          },
        },
      ],
    },
  })
  expect(kubectlAs(joeToken, ingress, 'apply', '-f', '-').ok).toBe(false)
  // Not in a Workspace his groups have no role in.
  expect(kubectlAs(joeToken, '', '-n', wind.ns, 'get', 'pods').ok).toBe(false)

  // Bob, a Viewer: reads, writes nothing.
  expect(kubectlAs(bobToken, '', '-n', solar.ns, 'get', 'pods', '-o', 'name').out).toContain('pod/joe')
  expect(kubectlAs(bobToken, pod(solar.ns, 'bob'), 'apply', '-f', '-').ok).toBe(false)
  expect(kubectlAs(bobToken, '', '-n', solar.ns, 'delete', 'pod', 'joe').ok).toBe(false)
  expect(kubectlAs(bobToken, '', '-n', wind.ns, 'get', 'pods').ok).toBe(false)

  // The portal's kubeconfig names the same proxy and asks kubelogin for the token.
  const kubeconfig = await joe.page.evaluate(
    async (id) => (await fetch(`/api/v1/workspaces/${id}/kubeconfig`)).text(),
    solar.id,
  )
  expect(kubeconfig).toContain(`server: ${proxy}`)
  expect(kubeconfig).toContain('oidc-login')

  // Headlamp: Joe gets in without a password, on his Namespace; Nodes are not his.
  const headlamp = await openConsole(joe.page, solar.id)
  await expect(headlamp.getByText(solar.ns).first()).toBeVisible({ timeout: 60_000 })
  await headlamp.screenshot({ path: test.info().outputPath('headlamp-developer.png'), fullPage: true })
  await headlamp.goto(new URL('/c/cluster-a/nodes', headlamp.url()).toString())
  await expect(headlamp.getByText(/fdp-k8s-wk/)).toHaveCount(0, { timeout: 30_000 })
  await headlamp.screenshot({ path: test.info().outputPath('headlamp-nodes.png'), fullPage: true })

  await cleanUp()
})

test('the console signs in someone in 20 groups', async ({ browser }) => {
  test.setTimeout(20 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2egr', 1, 25)
  const page = owner.page
  const ownerId = (await api(page, 'GET', '/me')).body.id as string
  const ws = await workspace(page, slug, 'solar', [{ groupId: admins.id, role: 'TenantOwner' }])

  // Twenty more groups, the owner in each, all in Keycloak.
  for (let i = 1; i <= 20; i++) {
    const g = await api(page, 'POST', `/organizations/${slug}/groups`, { name: `team-${i}` }, crypto.randomUUID())
    expect(g.status, JSON.stringify(g.body)).toBe(201)
    const m = await api(page, 'PUT', `/groups/${g.body.id}/members/${ownerId}`)
    expect([200, 201, 204], JSON.stringify(m.body)).toContain(m.status)
  }
  await until(
    'every group and membership in Keycloak',
    async () => {
      const r = await api(page, 'GET', `/organizations/${slug}/groups`)
      const gs = r.body.items as { syncStatus: string; members: { userId: string; syncStatus: string }[] }[]
      return gs.length >= 21 &&
        gs.every((g) => g.syncStatus === 'Synced' && g.members.every((m) => m.syncStatus === 'Synced'))
        ? true
        : undefined
    },
    5 * 60_000,
  )
  const token = (await kubectlTokens(page)).id_token
  const claims = JSON.parse(Buffer.from(token.split('.')[1]!, 'base64url').toString()) as { groups?: string[] }
  expect((claims.groups ?? []).filter((g) => g.startsWith('org_')).length).toBeGreaterThanOrEqual(21)

  const headlamp = await openConsole(page, ws.id)
  // The cookies are reported whether Headlamp lets the person in or not:
  // a browser drops a cookie over about 4 KiB.
  const report = async () => {
    const host = new URL(headlamp.url()).host
    const cookies = (await headlamp.context().cookies()).filter((c) => host.endsWith(c.domain.replace(/^\./, '')))
    const stored = await headlamp.evaluate(() =>
      Object.keys(localStorage).map((k) => `localStorage ${k}=${k.length + (localStorage.getItem(k) ?? '').length}`),
    )
    const sizes = [
      ...cookies.map((c) => `${c.name}${c.path === '/' ? '' : ` (path ${c.path})`}=${c.name.length + c.value.length}`),
      ...stored,
    ].join(', ')
    test.info().annotations.push({ type: 'console cookies (bytes)', description: sizes })
    console.log(`console cookies for ${claims.groups?.length} groups, token ${token.length} bytes: ${sizes}`)
  }
  try {
    await expect(headlamp.getByText(ws.ns).first()).toBeVisible({ timeout: 60_000 })
  } finally {
    await report()
  }
  await headlamp.screenshot({ path: test.info().outputPath('headlamp-20-groups.png'), fullPage: true })

  await cleanUp()
})

test('someone taken out of a group loses the portal at once and Kubernetes within 5 minutes', async ({ browser }) => {
  test.setTimeout(20 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2erv')
  const page = owner.page
  const groups = (await api(page, 'GET', `/organizations/${slug}/groups`)).body.items as { id: string; name: string }[]
  const developers = groups.find((g) => g.name === 'developers')!
  await until('developers in Keycloak', async () => {
    const r = await api(page, 'GET', `/organizations/${slug}/groups`)
    return (r.body.items as { name: string; syncStatus: string }[]).every((g) => g.syncStatus === 'Synced')
      ? true
      : undefined
  })
  const solar = await workspace(page, slug, 'solar', [
    { groupId: admins.id, role: 'TenantOwner' },
    { groupId: developers.id, role: 'Developer' },
  ])
  const joe = await member(browser, page, slug, [developers])
  const before = await kubectlTokens(joe.page)
  const exp = (JSON.parse(Buffer.from(before.id_token.split('.')[1]!, 'base64url').toString()) as { exp: number }).exp
  expect(kubectlAs(before.id_token, '', '-n', solar.ns, 'get', 'pods').ok).toBe(true)

  const removed = Date.now()
  const r = await api(page, 'DELETE', `/groups/${developers.id}/members/${joe.userId}`)
  expect([200, 204], JSON.stringify(r.body)).toContain(r.status)
  // The portal: at once.
  expect((await api(joe.page, 'GET', `/workspaces/${solar.id}`)).status).toBe(404)
  // Keycloak follows within a minute: a new token has no developers group and gets nothing.
  const machine = (groups as { id: string; machineName?: string }[]).find((g) => g.id === developers.id)!.machineName!
  const after = await until('a token without developers', async () => {
    const t = (await kubectlTokens(joe.page)).id_token
    return groupsOf(t).includes(machine) ? undefined : t
  })
  expect(kubectlAs(after, '', '-n', solar.ns, 'get', 'pods').ok).toBe(false)
  // The token issued before works until it expires, and not after.
  const end = await until(
    'the old token refused',
    async () => (kubectlAs(before.id_token, '', '-n', solar.ns, 'get', 'pods').ok ? undefined : Date.now()),
    7 * 60_000,
  )
  const seconds = Math.round((end - removed) / 1000)
  test.info().annotations.push({ type: 'Kubernetes access ended after (s)', description: String(seconds) })
  console.log(
    `revocation: removed at ${new Date(removed).toISOString()}, old token refused at ${new Date(end).toISOString()}, ${seconds} s; it expired at ${new Date(exp * 1000).toISOString()}`,
  )
  expect(seconds).toBeLessThanOrEqual(5 * 60 + 30)
  expect(end / 1000).toBeGreaterThanOrEqual(exp - 30) // it lasted as long as the token, no longer

  await cleanUp()
})
