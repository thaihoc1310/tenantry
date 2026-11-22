// M11's Routes on the cluster (IMPLEMENTATION-PLAN M11, "Test bắt buộc"): an
// owner publishes a Service from the portal and it answers over HTTPS at
// <name>-<workspace>.<apps domain>; a Route to a Service that is not there
// says why; the same hostname twice is refused; a tenant cannot write an
// Ingress itself; deleting the Namespace takes its Route and Ingress with it.
import { expect, test } from '@playwright/test'
import {
  api,
  cleanUp,
  cleanupAfterAll,
  deleteWorkspace,
  kubectl,
  kubectlIn,
  networkReady,
  onCleanup,
  succeeded,
  until,
  workspaceOrganization,
} from './support'

cleanupAfterAll()

const app = (ns: string) =>
  [
    {
      apiVersion: 'v1',
      kind: 'Pod',
      metadata: { name: 'web', namespace: ns, labels: { app: 'web' } },
      spec: {
        securityContext: { runAsNonRoot: true, seccompProfile: { type: 'RuntimeDefault' } },
        containers: [
          {
            name: 'nginx',
            image: 'docker.io/nginxinc/nginx-unprivileged:1.27-alpine',
            ports: [{ containerPort: 8080 }],
            resources: { requests: { cpu: '50m', memory: '32Mi' }, limits: { cpu: '100m', memory: '64Mi' } },
            securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: ['ALL'] } },
          },
        ],
      },
    },
    {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: { name: 'web', namespace: ns },
      spec: { selector: { app: 'web' }, ports: [{ port: 80, targetPort: 8080 }] },
    },
  ]
    .map((o) => JSON.stringify(o))
    .join('\n')

test('a Route publishes a Service over HTTPS and goes with its Namespace', async ({ browser }) => {
  test.setTimeout(20 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2ert')
  const page = owner.page
  const k8sName = `${slug}-solar`
  const ns = `${k8sName}-frontend`
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
        requestsCpu: '250m',
        requestsMemory: '256Mi',
        limitsCpu: '500m',
        limitsMemory: '512Mi',
        pods: 5,
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
  onCleanup('Workspace solar', () => deleteWorkspace(page, wsId, 'solar', k8sName))
  await succeeded(page, res.body.operation.id)
  const asOwner = [
    `--as=oidc:${owner.who.username}`,
    '--as-group=oidc:tenantry:tenant-users',
    `--as-group=oidc:${admins.machineName}`,
    '--as-group=system:authenticated',
  ]
  await networkReady(ns)

  // A Route to a Service not there yet: the portal says why.
  await page.goto(`/orgs/${slug}/workspaces/solar?tab=routes`)
  await page.getByRole('button', { name: 'Create Route' }).click()
  const dialog = page.getByRole('dialog', { name: 'Create Route' })
  await dialog.locator('#rt-name').fill('web')
  await dialog.locator('#rt-svc').fill('web')
  await expect(dialog.getByText(`https://web-${k8sName}.`)).toBeVisible()
  await dialog.getByRole('button', { name: 'Create Route' }).click()
  const row = page.getByRole('row', { name: new RegExp(`web-${k8sName}`) })
  await expect(row).toContainText('Service web does not exist', { timeout: 3 * 60_000 })
  await page.screenshot({ path: test.info().outputPath('route-not-ready.png'), fullPage: true })

  // The app arrives: the Route turns Active and the address answers.
  kubectlIn(app(ns), 'apply', '-f', '-', ...asOwner)
  await expect(row).toContainText('Active', { timeout: 3 * 60_000 })
  const url = (await api(page, 'GET', `/workspaces/${wsId}/routes`)).body.items[0].url as string
  expect(url).toMatch(new RegExp(`^https://web-${k8sName}\\.`))
  const body = await until(
    'the address answering',
    async () => {
      try {
        const r = await fetch(url)
        return r.status === 200 ? await r.text() : undefined
      } catch {
        return undefined // DNS or the Pod not up yet
      }
    },
    5 * 60_000,
  )
  expect(body).toContain('nginx')
  await page.screenshot({ path: test.info().outputPath('route-active.png'), fullPage: true })
  // The Ingress is the operator's, in the tenant's Namespace, with no TLS secret there.
  const ing = JSON.parse(kubectl('-n', ns, 'get', 'ingress', 'web', '-o', 'json'))
  expect(ing.spec.rules[0].host).toBe(new URL(url).host)
  expect(ing.spec.tls?.[0]?.secretName).toBeUndefined()

  // The same hostname again is refused (once the Workspace takes changes again).
  await until('the Workspace Ready', async () =>
    (await api(page, 'GET', `/workspaces/${wsId}`)).body.state === 'Ready' ? true : undefined,
  )
  const again = await api(
    page,
    'POST',
    `/workspaces/${wsId}/routes`,
    { namespace: ns, name: 'web', service: 'web', port: 80 },
    crypto.randomUUID(),
  )
  expect(again.status, JSON.stringify(again.body)).toBe(409)
  expect(again.body.code).toBe('ROUTE_HOSTNAME_TAKEN')

  // A tenant writing an Ingress is refused at admission.
  let refused = ''
  try {
    kubectlIn(
      JSON.stringify({
        apiVersion: 'networking.k8s.io/v1',
        kind: 'Ingress',
        metadata: { name: 'mine', namespace: ns },
        spec: {
          rules: [
            {
              host: `mine-${k8sName}.example.com`,
              http: {
                paths: [{ path: '/', pathType: 'Prefix', backend: { service: { name: 'web', port: { number: 80 } } } }],
              },
            },
          ],
        },
      }),
      'apply',
      '-f',
      '-',
      ...asOwner,
    )
  } catch (e) {
    refused = String((e as { stderr?: string }).stderr ?? e)
  }
  expect(refused).not.toBe('')

  // Deleting the Namespace takes the Route and its Ingress.
  const del = await api(page, 'DELETE', `/workspaces/${wsId}/namespaces/${ns}`)
  expect(del.status, JSON.stringify(del.body)).toBe(202)
  expect((await api(page, 'GET', `/workspaces/${wsId}/routes`)).body.items).toEqual([])
  await succeeded(page, del.body.operation.id)
  await until('the namespace gone', async () =>
    kubectl('get', 'namespace', ns, '--ignore-not-found') === '' ? true : undefined,
  )
  await cleanUp()
})
