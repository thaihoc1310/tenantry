// M6's acceptance on the cluster (IMPLEMENTATION-PLAN M6, "Đầu ra kiểm chứng"):
// a Workspace created through the API reaches the cluster as a Capsule
// Tenant whose owners are the TenantOwner group; a Namespace asked for on the
// portal is created; one created with kubectl by a member of that group
// shows up with source kubectl. The Workspace is deleted at the end.
import { expect, test } from '@playwright/test'
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

cleanupAfterAll()

test('a Workspace reaches the cluster with its owners and Namespaces', async ({ browser }) => {
  test.setTimeout(10 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2ews')
  const page = owner.page
  const k8sName = `${slug}-solar`
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
  const wsId: string = res.body.workspace.id
  onCleanup('Workspace solar', () => deleteWorkspace(page, wsId, 'solar', k8sName))
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
  await cleanUp()
})
