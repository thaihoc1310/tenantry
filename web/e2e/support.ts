// What the end-to-end tests share: new accounts, Mailpit, logging in.
import { execFileSync } from 'node:child_process'
import { expect, test, type Browser, type Page } from '@playwright/test'

export const mailpit = process.env.E2E_MAILPIT ?? 'http://127.0.0.1:8025'

export function adminPassword(): string {
  const p = process.env.E2E_ADMIN_PASSWORD
  if (!p) throw new Error('E2E_ADMIN_PASSWORD is not set (make e2e reads it from the cluster)')
  return p
}

// A person who does not exist yet: unique per run, recognisable as a test.
export function newcomer() {
  const id = `e2e${Date.now().toString(36)}${Math.floor(Math.random() * 1e4).toString(36)}`
  return { username: id, email: `${id}@tenantry.site`, password: `Pw-${crypto.randomUUID()}`, first: 'E2E', last: id }
}

// verificationLink waits for Keycloak's mail to email and returns its link.
export async function verificationLink(email: string): Promise<string> {
  for (let i = 0; i < 30; i++) {
    try {
      const res = await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${email}"`)}`)
      const { messages } = (await res.json()) as { messages: { ID: string }[] }
      if (messages.length) {
        const msg = (await (await fetch(`${mailpit}/api/v1/message/${messages[0]!.ID}`)).json()) as { Text: string }
        const link = /https:\/\/\S+\/login-actions\/action-token\?\S+/.exec(msg.Text)?.[0]
        if (link) return link
      }
    } catch {
      // The port-forward to Mailpit drops a connection now and then: ask again.
    }
    await new Promise((r) => setTimeout(r, 1000))
  }
  throw new Error(`no verification mail for ${email} in Mailpit`)
}

// fillSecret types a secret into a field without it landing in the trace:
// Playwright records a fill's value as the action's title, so a password
// typed with fill sits in every kept trace.zip. The value reaches the page
// through a binding instead: the trace keeps the binding's name and the page
// script, not what the binding returns. DOM snapshots would still keep the
// field's value: the suite records none (e2e/trace.ts; trace-secrets.spec
// checks both together).
let secrets = 0
export async function fillSecret(page: Page, selector: string, secret: string) {
  const fn = `__tenantrySecret${++secrets}`
  await page.exposeFunction(fn, () => secret)
  await page.locator(selector).evaluate(async (el, name) => {
    const input = el as HTMLInputElement
    input.value = await (window as unknown as Record<string, () => Promise<string>>)[name]!()
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  }, fn)
}

// signUp registers a new account through Keycloak's own pages: the form asks
// no password while the realm verifies email (Keycloak 26), the link in the
// mail does, so only the owner of the address can set one. It ends on the
// portal, logged in.
export async function signUp(browser: Browser) {
  const who = newcomer()
  const page = await (await browser.newContext()).newPage()
  await page.goto('/')
  await page.getByRole('link', { name: /register/i }).click()
  await page.fill('#username', who.username)
  await page.fill('#email', who.email)
  await page.fill('#firstName', who.first)
  await page.fill('#lastName', who.last)
  await page.getByRole('button', { name: /register/i }).click()
  // Not logged in to the portal before the address is verified.
  await expect(page.getByText(/verify your email/i).first()).toBeVisible()
  await page.goto(await verificationLink(who.email))
  await fillSecret(page, '#password-new', who.password)
  await fillSecret(page, '#password-confirm', who.password)
  await page.getByRole('button', { name: /submit/i }).click()
  await page.waitForURL((u) => !u.pathname.startsWith('/realms/'))
  return { page, who }
}

// logIn opens a new browser context and logs in at Keycloak.
export async function logIn(browser: Browser, username: string, password: string, path = '/'): Promise<Page> {
  const page = await (await browser.newContext()).newPage()
  await page.goto(path)
  await page.fill('#username', username)
  await fillSecret(page, '#password', password)
  await page.click('#kc-login')
  await page.waitForURL((u) => !u.pathname.startsWith('/realms/'))
  return page
}

// requestOrganization fills the request form; it returns the slug.
export async function requestOrganization(page: Page, name: string): Promise<string> {
  await expect(page.getByRole('heading', { name: 'Request an Organization' })).toBeVisible()
  await page.fill('#org-name', name)
  await page.fill('#org-purpose', 'End-to-end test of the onboarding flow.')
  const slug = await page.inputValue('#org-slug')
  await page.getByRole('button', { name: 'Send request' }).click()
  await expect(page.getByRole('heading', { name: 'Your request is waiting for review' })).toBeVisible()
  return slug
}

// openReview opens the review page of the request with this slug.
export async function openReview(admin: Page, slug: string) {
  await admin.goto('/admin/organizations')
  const row = admin.locator('.el-table__row', { hasText: slug })
  await row.getByRole('button', { name: 'Review' }).click()
  await expect(admin.getByText('Starting grant on cluster-a')).toBeVisible()
}

// api calls the Platform API from a logged-in page, with the CSRF header the
// portal sends and, when given, an Idempotency-Key.
export async function api(page: Page, method: string, path: string, body?: unknown, idempotencyKey?: string) {
  return page.evaluate(
    async ([method, path, body, key]) => {
      const csrf = document.cookie
        .split('; ')
        .find((c) => c.startsWith('__Host-tenantry_csrf='))
        ?.split('=')[1]
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        'X-CSRF-Token': decodeURIComponent(csrf ?? ''),
      }
      if (key) headers['Idempotency-Key'] = key
      const res = await fetch('/api/v1' + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      })
      return { status: res.status, body: res.status === 204 ? null : await res.json() }
    },
    [method, path, body, idempotencyKey] as const,
  )
}

// kubectl runs against the lab with the admin's kubeconfig context;
// kubectlIn feeds it input (a manifest for -f -).
export const kubectl = (...args: string[]) => kubectlIn('', ...args)
export const kubectlIn = (input: string, ...args: string[]) =>
  execFileSync('kubectl', ['--context', process.env.E2E_KUBE_CONTEXT ?? 'hocbt@kubernetes', ...args], {
    input,
    encoding: 'utf8',
    stdio: ['pipe', 'pipe', 'pipe'],
  })

// until polls f until it returns a value, for up to timeout ms.
export async function until<T>(what: string, f: () => Promise<T | undefined>, timeout = 180_000): Promise<T> {
  const end = Date.now() + timeout
  for (;;) {
    const v = await f()
    if (v !== undefined) return v
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 2000))
  }
}

// succeeded waits for an operation to succeed; any other end fails the test.
export async function succeeded(page: Page, opId: string) {
  return until(`operation ${opId}`, async () => {
    const r = await api(page, 'GET', `/operations/${opId}`)
    expect(['Pending', 'Running', 'Succeeded'], JSON.stringify(r.body)).toContain(r.body.state)
    return r.body.state === 'Succeeded' ? r.body : undefined
  })
}

// workspaceOrganization signs up an owner, has the Platform Admin create an
// active Organization for them with a grant on cluster-a, and waits for its
// group admins to reach Keycloak, so it can be bound. maxWorkspaces bounds
// both the entitlement and the grant.
export async function workspaceOrganization(browser: Browser, prefix: string, maxWorkspaces = 2, maxGroups = 5) {
  const owner = await signUp(browser)
  const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
  const slug = `${prefix}${Date.now().toString(36).slice(-6)}`
  const created = await api(
    admin,
    'POST',
    '/admin/organizations',
    {
      slug,
      displayName: `E2E ${slug}`,
      description: 'End-to-end test of Workspaces.',
      ownerEmail: owner.who.email,
      entitlement: { maxMembers: 5, maxGroups, maxWorkspacesTotal: maxWorkspaces },
      clusterGrant: {
        clusterId: 'cluster-a',
        maxWorkspaces,
        maxNamespacesTotal: 6,
        resources: {
          requestsCpu: '2',
          requestsMemory: '4Gi',
          limitsCpu: '4',
          limitsMemory: '8Gi',
          pods: 20,
          requestsStorage: '10Gi',
        },
        sharedPoolIds: ['shared-general'],
        networkPoolIds: ['onboarding-pod-network'],
        allowedStorageClasses: ['standard'],
      },
    },
    crypto.randomUUID(),
  )
  expect(created.status, JSON.stringify(created.body)).toBe(201)
  cleanUpOrganization(browser, created.body.organization.id, slug)
  await owner.page.reload()
  const admins = await until('group admins in Keycloak', async () => {
    const r = await api(owner.page, 'GET', `/organizations/${slug}/groups`)
    const g = r.body.items?.find((x: { name: string }) => x.name === 'admins')
    return g?.syncStatus === 'Synced' ? (g as { id: string; machineName: string }) : undefined
  })
  return { owner, admin, slug, admins }
}

// cleanUpOrganization removes a test's Organization at its end, so a run
// leaves none behind: as the Platform Admin (the owner may have logged out
// or handed it over), a request still Pending is rejected and an Active
// Organization deleted. Register it before the test's other cleanups: they
// run last first, so its Workspaces and dedicated nodes are gone by then.
// Deleting waits for the Workspaces' address blocks to leave quarantine (10
// minutes); a test does not wait that long: refused for that alone, the
// Organization is left, said so, to make demo-purge-test-data.
export function cleanUpOrganization(browser: Browser, id: string | (() => string | undefined), label = '') {
  onCleanup(`organization ${label}`, async () => {
    const orgId = typeof id === 'function' ? id() : id
    if (!orgId) return
    const admin = await logIn(browser, 'admin', adminPassword(), '/admin/organizations')
    try {
      const got = await api(admin, 'GET', `/admin/organizations/${orgId}`)
      if (got.status === 404) return // deleted already
      expect(got.status, JSON.stringify(got.body)).toBe(200)
      const status = got.body.organization.status as string
      if (status === 'Pending') {
        const r = await api(
          admin,
          'POST',
          `/admin/organizations/${orgId}:reject`,
          { reason: 'end-to-end test cleanup' },
          crypto.randomUUID(),
        )
        expect(r.status, JSON.stringify(r.body)).toBe(200)
        return
      }
      if (status !== 'Active') return // Rejected: nothing to remove through the API
      const r = await api(admin, 'DELETE', `/admin/organizations/${orgId}`)
      const d = r.body?.details ?? {}
      if (r.status === 409 && r.body.code === 'ORGANIZATION_NOT_EMPTY' && !d.workspaces && !d.dedicatedNodes) {
        console.log(
          `organization ${label || orgId} left until ${d.deletableAfter}: its address blocks are in quarantine`,
        )
        return
      }
      expect(r.status, JSON.stringify(r.body)).toBe(204)
    } finally {
      await admin.context().close()
    }
  })
}

// orgIdOf reads the id of an Organization its member can see.
export async function orgIdOf(page: Page, slug: string): Promise<string> {
  const r = await api(page, 'GET', `/organizations/${slug}`)
  expect(r.status, JSON.stringify(r.body)).toBe(200)
  return r.body.id as string
}

// deleteWorkspace deletes a Workspace through the API and waits until the
// cluster has nothing of it left: no PlatformWorkspace, Tenant or Namespace.
// A Workspace busy with a change (409) is asked again until it takes it.
export async function deleteWorkspace(page: Page, wsId: string, slug: string, k8sName: string) {
  const r = await until(
    `delete of ${k8sName} accepted`,
    async () => {
      const r = await api(page, 'DELETE', `/workspaces/${wsId}?confirmName=${slug}`)
      return r.status === 409 && r.body.code === 'WORKSPACE_NOT_READY' ? undefined : r
    },
    10 * 60_000,
  )
  if (r.status === 202) await succeeded(page, r.body.operation.id)
  else expect(r.status, JSON.stringify(r.body)).toBe(404) // gone already
  expect((await api(page, 'GET', `/workspaces/${wsId}`)).status).toBe(404)
  expect(kubectl('get', 'platformworkspace', k8sName, '--ignore-not-found')).toBe('')
  expect(kubectl('get', 'tenants.capsule.clastix.io', k8sName, '--ignore-not-found')).toBe('')
  expect(kubectl('get', 'namespaces', '-l', `capsule.clastix.io/tenant=${k8sName}`, '-o', 'name')).toBe('')
}

// networkReady waits until a tenant Namespace of a Workspace with a Pod
// network has its baseline NetworkPolicy: before that the
// tenantry-tenant-network-ready policy refuses its Pods (architecture/05 §6).
export async function networkReady(ns: string) {
  await until(`the network of ${ns}`, async () =>
    kubectl('-n', ns, 'get', 'networkpolicy', 'tenant-default-deny', '--ignore-not-found', '-o', 'name')
      ? true
      : undefined,
  )
}

// Cleanup that survives a failing or timed-out test (a finally block shares
// the test's timeout, so a test stuck until its timeout never got to it). A
// test registers what to undo as soon as it has made it; the test runs
// cleanUp() at its end, and cleanupAfterAll() runs whatever is left after a
// failure, in a hook with a timeout of its own. Each step must be safe to
// run twice.
const pending: [string, () => Promise<void>][] = []

export function onCleanup(what: string, run: () => Promise<void>) {
  pending.push([what, run])
}

// cleanUp runs the registered steps, last first, all of them even when one fails.
export async function cleanUp() {
  const failed: string[] = []
  while (pending.length) {
    const [what, run] = pending.pop()!
    try {
      await run()
    } catch (e) {
      failed.push(`${what}: ${e instanceof Error ? e.message : String(e)}`)
    }
  }
  if (failed.length) throw new Error(`cleanup failed:\n${failed.join('\n')}`)
}

export function cleanupAfterAll() {
  test.afterAll(async () => {
    test.setTimeout(15 * 60_000)
    await cleanUp()
  })
}

// ---- Keycloak tokens of the kubectl client --------------------------------
// The kubectl client is public: authorization code with PKCE and a loopback
// redirect, as kubelogin does. The browser context is logged in to Keycloak,
// so the code comes back at once.
const issuer = 'https://auth.tenantry.site/realms/tenantry'
const loopback = 'http://localhost:8000'

function b64url(bytes: ArrayBuffer | Uint8Array): string {
  return Buffer.from(bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes)).toString('base64url')
}

export async function kubectlTokens(
  portal: Page,
): Promise<{ id_token: string; refresh_token: string; access_token: string }> {
  const page = await portal.context().newPage() // same Keycloak session, the portal tab stays where it is
  const verifier = b64url(crypto.getRandomValues(new Uint8Array(32)))
  const challenge = b64url(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier)))
  const q = new URLSearchParams({
    client_id: 'kubectl',
    response_type: 'code',
    scope: 'openid',
    redirect_uri: loopback,
    code_challenge: challenge,
    code_challenge_method: 'S256',
    state: b64url(crypto.getRandomValues(new Uint8Array(8))),
  })
  // Nothing listens on the loopback address: the navigation fails there, and
  // the code is read from the request (page.route does not see redirects).
  const back = page.waitForRequest((r) => r.url().startsWith(loopback))
  await page.goto(`${issuer}/protocol/openid-connect/auth?${q}`).catch(() => {})
  const code = new URL((await back).url()).searchParams.get('code') ?? ''
  expect(code).not.toBe('')
  await page.close()
  return token({
    grant_type: 'authorization_code',
    client_id: 'kubectl',
    code,
    redirect_uri: loopback,
    code_verifier: verifier,
  })
}

export async function token(form: Record<string, string>) {
  const res = await fetch(`${issuer}/protocol/openid-connect/token`, {
    method: 'POST',
    body: new URLSearchParams(form),
  })
  expect(res.status, await res.clone().text()).toBe(200)
  return (await res.json()) as { id_token: string; refresh_token: string; access_token: string }
}

export function groupsOf(jwt: string): string[] {
  const claims = JSON.parse(Buffer.from(jwt.split('.')[1]!, 'base64url').toString()) as { groups?: string[] }
  return claims.groups ?? []
}

// member signs a newcomer up and brings them into the organization through
// an invitation naming groups; it returns once Keycloak has every membership,
// so the person's next token names the groups.
export async function member(browser: Browser, owner: Page, slug: string, groups: { id: string; name: string }[]) {
  const who = await signUp(browser)
  const inv = await api(
    owner,
    'POST',
    `/organizations/${slug}/invitations`,
    { email: who.who.email, role: 'OrgMember', groupIds: groups.map((g) => g.id) },
    crypto.randomUUID(),
  )
  expect(inv.status, JSON.stringify(inv.body)).toBe(201)
  const token = String(inv.body.link).split('/invitations/')[1]!
  const acc = await api(who.page, 'POST', `/invitations/${token}:accept`, undefined, crypto.randomUUID())
  expect(acc.status, JSON.stringify(acc.body)).toBe(200)
  const userId = (await api(who.page, 'GET', '/me')).body.id as string
  await until(`${who.who.username} in Keycloak`, async () => {
    const r = await api(owner, 'GET', `/organizations/${slug}/groups`)
    const mine = (r.body.items as { id: string; members: { userId: string; syncStatus: string }[] }[]).filter((g) =>
      groups.some((x) => x.id === g.id),
    )
    return mine.every((g) => g.members.some((m) => m.userId === userId && m.syncStatus === 'Synced')) ? true : undefined
  })
  return { ...who, userId }
}
