// M7's acceptance on the cluster (IMPLEMENTATION-PLAN M7, "Đầu ra kiểm chứng"):
// a Workspace's quota is enforced on Pods with Capsule's message; resizing it
// lets the Pod in; cordoning freezes it; deleting a Workspace with a volume
// leaves no Namespace, volume claim or Tenant, and gives its share back to
// the grant. Pods state requests, as every Pod of a Tenant must.
import { expect, test } from '@playwright/test'
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

const pod = (ns: string, name: string, cpu: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name, namespace: ns },
    spec: {
      containers: [
        {
          name: 'pause',
          image: 'registry.k8s.io/pause:3.10',
          resources: { requests: { cpu, memory: '64Mi' }, limits: { cpu, memory: '64Mi' } },
        },
      ],
    },
  })

const claim = (ns: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'PersistentVolumeClaim',
    metadata: { name: 'data', namespace: ns },
    spec: {
      accessModes: ['ReadWriteOnce'],
      storageClassName: 'standard',
      resources: { requests: { storage: '100Mi' } },
    },
  })

test('quota, resize, cordon and delete of a Workspace on the cluster', async ({ browser }) => {
  test.setTimeout(15 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2elc')
  const page = owner.page
  const k8sName = `${slug}-solar`
  const ns = `${k8sName}-frontend`
  // As a member of the TenantOwner group, through Capsule.
  const asOwner = [
    `--as=oidc:${owner.who.username}`,
    '--as-group=oidc:tenantry:tenant-users',
    `--as-group=oidc:${admins.machineName}`,
    '--as-group=system:authenticated',
  ]
  const apply = (manifest: string) => {
    try {
      return { ok: true, out: kubectlIn(manifest, 'apply', '-f', '-', ...asOwner) }
    } catch (e) {
      return { ok: false, out: String((e as { stderr?: string }).stderr ?? e) }
    }
  }

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
        requestsCpu: '200m',
        requestsMemory: '512Mi',
        limitsCpu: '200m',
        limitsMemory: '512Mi',
        pods: 4,
        requestsStorage: '1Gi',
        namespaceQuota: 2,
      },
      namespaces: ['frontend'],
      bindings: [{ groupId: admins.id, role: 'TenantOwner' }],
    },
    crypto.randomUUID(),
  )
  expect(res.status, JSON.stringify(res.body)).toBe(202)
  const wsId: string = res.body.workspace.id
  let volume = ''
  try {
    await succeeded(page, res.body.operation.id)
    await networkReady(ns)

    // 300m does not fit in a quota of 200m: Capsule says so.
    const refused = apply(pod(ns, 'big', '300m'))
    expect(refused.ok, refused.out).toBe(false)
    expect(refused.out).toMatch(/exceed|quota/i)

    // Resize to 1 CPU: the same Pod is admitted.
    const r = await api(page, 'PATCH', `/workspaces/${wsId}`, {
      resources: {
        requestsCpu: '1',
        requestsMemory: '1Gi',
        limitsCpu: '1',
        limitsMemory: '1Gi',
        pods: 4,
        requestsStorage: '1Gi',
        namespaceQuota: 2,
      },
    })
    expect(r.status, JSON.stringify(r.body)).toBe(202)
    await succeeded(page, r.body.operation.id)
    await until(
      'the Pod admitted after the resize',
      async () => (apply(pod(ns, 'big', '300m')).ok ? true : undefined),
      60_000,
    )

    // The grant counts the larger share.
    const usage = await api(page, 'GET', `/organizations/${slug}/usage`)
    expect(usage.body.clusterGrants[0].requestsCpu.allocated).toBe('1')

    // Cordoned: tenants change nothing; uncordon gives it back.
    const c = await api(page, 'POST', `/workspaces/${wsId}:cordon`)
    expect(c.status, JSON.stringify(c.body)).toBe(202)
    await succeeded(page, c.body.operation.id)
    expect((await api(page, 'GET', `/workspaces/${wsId}`)).body.state).toBe('Cordoned')
    expect(apply(pod(ns, 'small', '50m')).ok).toBe(false)
    const u = await api(page, 'POST', `/workspaces/${wsId}:uncordon`)
    await succeeded(page, u.body.operation.id)

    // A volume to lose with the Workspace.
    const pvc = apply(claim(ns))
    expect(pvc.ok, pvc.out).toBe(true)
    volume = await until(
      'the claim bound',
      async () => kubectl('-n', ns, 'get', 'pvc', 'data', '-o', 'jsonpath={.spec.volumeName}').trim() || undefined,
    )
    await page.goto(`/orgs/${slug}/workspaces/solar?tab=resources`)
    await expect(page.getByText('Quota of this Workspace')).toBeVisible()
    if (process.env.E2E_SHOTS)
      await page.screenshot({ path: `${process.env.E2E_SHOTS}/workspace-resources.png`, fullPage: true })
  } finally {
    await deleteWorkspace(page, wsId, 'solar', k8sName)
  }
  expect(kubectl('get', 'pvc', '-A', '--field-selector', `metadata.namespace=${ns}`, '-o', 'name')).toBe('')
  // Reclaim policy Delete: the volume goes too.
  if (volume)
    await until('the volume deleted', async () =>
      kubectl('get', 'pv', volume, '--ignore-not-found') === '' ? true : undefined,
    )
  const after = await api(page, 'GET', `/organizations/${slug}/usage`)
  expect(after.body.clusterGrants[0].workspaces.allocated).toBe(0)
})
