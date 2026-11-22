// M6's acceptance on the cluster (IMPLEMENTATION-PLAN M6, "Đầu ra kiểm chứng"):
// a Workspace created through the API reaches the cluster as a Capsule
// Tenant whose owners are the TenantOwner group; a Namespace asked for on the
// portal is created; one created with kubectl by a member of that group
// shows up with source kubectl. The Workspace is removed from the cluster and
// the database at the end (deleting Workspaces comes with M7).
import { execFileSync } from 'node:child_process'
import { expect, test, type Page } from '@playwright/test'
import { adminPassword, api, logIn, signUp } from './support'

const context = process.env.E2E_KUBE_CONTEXT ?? 'hocbt@kubernetes'
const kubectl = (...args: string[]) =>
  execFileSync('kubectl', ['--context', context, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })

// until polls f until it returns a value, for up to timeout ms.
async function until<T>(what: string, f: () => Promise<T | undefined>, timeout = 180_000): Promise<T> {
  const end = Date.now() + timeout
  for (;;) {
    const v = await f()
    if (v !== undefined) return v
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 2000))
  }
}

async function succeeded(page: Page, opId: string) {
  return until(`operation ${opId}`, async () => {
    const r = await api(page, 'GET', `/operations/${opId}`)
    expect(['Pending', 'Running', 'Succeeded'], JSON.stringify(r.body)).toContain(r.body.state)
    return r.body.state === 'Succeeded' ? r.body : undefined
  })
}

test('a Workspace reaches the cluster with its owners and Namespaces', async ({ browser }) => {
  test.setTimeout(10 * 60_000)
  const owner = await signUp(browser)
  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
  const slug = `e2ews${Date.now().toString(36).slice(-6)}`
  const created = await api(
    admin,
    'POST',
    '/admin/organizations',
    {
      slug,
      displayName: `E2E Workspace ${slug}`,
      description: 'End-to-end test of provisioning.',
      ownerEmail: owner.who.email,
      entitlement: { maxMembers: 5, maxGroups: 5, maxWorkspacesTotal: 2 },
      clusterGrant: {
        clusterId: 'cluster-a',
        maxWorkspaces: 2,
        maxNamespacesTotal: 6,
        resources: {
          requestsCpu: '1',
          requestsMemory: '2Gi',
          limitsCpu: '2',
          limitsMemory: '4Gi',
          pods: 20,
          requestsStorage: '5Gi',
        },
        sharedPoolIds: ['shared-general'],
        networkPoolIds: ['onboarding-pod-network'],
        allowedStorageClasses: ['standard'],
      },
    },
    crypto.randomUUID(),
  )
  expect(created.status, JSON.stringify(created.body)).toBe(201)

  // The owner's default group admins must reach Keycloak before it is bound.
  const page = owner.page
  await page.reload()
  const admins = await until('group admins in Keycloak', async () => {
    const r = await api(page, 'GET', `/organizations/${slug}/groups`)
    const g = r.body.items?.find((x: { name: string }) => x.name === 'admins')
    return g?.syncStatus === 'Synced' ? g : undefined
  })

  const k8sName = `${slug}-solar`
  let wsId: string | undefined
  try {
    const res = await api(
      page,
      'POST',
      `/organizations/${slug}/workspaces`,
      {
        slug: 'solar',
        displayName: 'Solar',
        clusterId: 'cluster-a',
        compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
        resources: {
          requestsCpu: '500m',
          requestsMemory: '1Gi',
          limitsCpu: '1',
          limitsMemory: '2Gi',
          pods: 10,
          requestsStorage: '1Gi',
          namespaceQuota: 3,
        },
        namespaces: ['frontend'],
        bindings: [{ groupId: admins.id, role: 'TenantOwner' }],
      },
      crypto.randomUUID(),
    )
    expect(res.status, JSON.stringify(res.body)).toBe(202)
    wsId = res.body.workspace.id
    await succeeded(page, res.body.operation.id)
    const ws = await api(page, 'GET', `/workspaces/${wsId}`)
    expect(ws.body.state).toBe('Ready')

    // The Tenant's owners: the TenantOwner group, and the operator.
    const tenant = JSON.parse(kubectl('get', 'tenants.capsule.clastix.io', k8sName, '-o', 'json'))
    const owners = tenant.spec.owners.map((o: { kind: string; name: string }) => `${o.kind}:${o.name}`)
    expect(owners).toContain(`Group:oidc:${admins.machineName}`)
    kubectl('get', 'namespace', `${k8sName}-frontend`)

    // A Namespace from the portal.
    const ns = await api(page, 'POST', `/workspaces/${wsId}/namespaces`, { name: 'backend' }, crypto.randomUUID())
    expect(ns.status, JSON.stringify(ns.body)).toBe(202)
    await succeeded(page, ns.body.operation.id)
    kubectl('get', 'namespace', `${k8sName}-backend`)

    // One made with kubectl by a member of the TenantOwner group.
    kubectl(
      'create',
      'namespace',
      `${k8sName}-scratch`,
      `--as=oidc:${owner.who.username}`,
      '--as-group=oidc:tenantry:tenant-users',
      `--as-group=oidc:${admins.machineName}`,
      '--as-group=system:authenticated',
    )
    const listed = await until('the kubectl Namespace on the portal', async () => {
      const r = await api(page, 'GET', `/workspaces/${wsId}/namespaces`)
      const found = r.body.items.find((n: { name: string }) => n.name === `${k8sName}-scratch`)
      return found?.state === 'Active' ? r.body.items : undefined
    })
    const sources = Object.fromEntries(listed.map((n: { name: string; source: string }) => [n.name, n.source]))
    expect(sources).toEqual({
      [`${k8sName}-backend`]: 'portal',
      [`${k8sName}-frontend`]: 'portal',
      [`${k8sName}-scratch`]: 'kubectl',
    })
  } finally {
    // The operator's finalizer deletes the Namespaces and the Tenant; the
    // row goes too, so the lab keeps no half Workspace.
    kubectl('delete', 'platformworkspace', k8sName, '--ignore-not-found', '--wait=true', '--timeout=180s')
    if (wsId)
      kubectl(
        '-n',
        'tenantry-system',
        'exec',
        'postgres-0',
        '--',
        'psql',
        '-U',
        'postgres',
        '-d',
        'tenantry',
        '-c',
        `DELETE FROM workspaces WHERE id = '${wsId}'`,
      )
  }
})
