// ADR-0006 on the lab: the web console (Headlamp) streams a Pod's log and
// opens a shell in it, both over WebSockets through Capsule Proxy, with the
// user's own token; and someone who logs out of Headlamp leaves nothing
// behind for the next person on the same browser: signing in again asks
// Keycloak for a login, and the next person sees their own Namespaces only.
import { expect, test } from '@playwright/test'
import {
  api,
  cleanUp,
  cleanupAfterAll,
  headlampSignIn,
  kubectlAs,
  kubectlTokens,
  logInAtKeycloak,
  member,
  networkReady,
  openConsole,
  sharedWorkspace,
  subjectOf,
  timed,
  until,
  workspaceOrganization,
} from './support'

cleanupAfterAll()

// A tiny Pod that writes a numbered line each second; requests equal limits.
const ticker = (ns: string) =>
  JSON.stringify({
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name: 'ticker', namespace: ns },
    spec: {
      containers: [
        {
          name: 'ticker',
          image: 'docker.io/library/busybox:1.36.1',
          command: ['sh', '-c', 'i=0; while true; do echo tick-$i; i=$((i+1)); sleep 1; done'],
          resources: { requests: { cpu: '50m', memory: '32Mi' }, limits: { cpu: '50m', memory: '32Mi' } },
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

const lastTick = (text: string) => Math.max(-1, ...[...text.matchAll(/tick-(\d+)/g)].map((m) => Number(m[1])))

test('Headlamp streams logs and opens a shell through the proxy; after logging out, the next person signs in as themselves', async ({
  browser,
}) => {
  test.setTimeout(25 * 60_000)
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2ehl')
  const page = owner.page
  const groups = await until('developers and viewers in Keycloak', async () => {
    const r = await api(page, 'GET', `/organizations/${slug}/groups`)
    const gs = r.body.items as { id: string; name: string; syncStatus: string }[]
    return gs.every((g) => g.syncStatus === 'Synced') ? gs : undefined
  })
  const developers = groups.find((g) => g.name === 'developers')!
  const viewers = groups.find((g) => g.name === 'viewers')!
  const solar = await sharedWorkspace(page, slug, 'solar', [
    { groupId: admins.id, role: 'TenantOwner' },
    { groupId: developers.id, role: 'Developer' },
  ])
  const wind = await sharedWorkspace(page, slug, 'wind', [
    { groupId: admins.id, role: 'TenantOwner' },
    { groupId: viewers.id, role: 'Viewer' },
  ])
  await networkReady(solar.ns)
  // Joe, a Developer of solar, and Bea, a Viewer of wind.
  const joe = await member(browser, page, slug, [developers])
  const bea = await member(browser, page, slug, [viewers])

  const joeToken = (await kubectlTokens(joe.page)).id_token
  const made = kubectlAs(joeToken, ticker(solar.ns), 'apply', '-f', '-')
  expect(made.ok, made.out).toBe(true)
  await until('the ticker Pod Running', async () =>
    kubectlAs(joeToken, '', '-n', solar.ns, 'get', 'pod', 'ticker', '-o', 'jsonpath={.status.phase}').out === 'Running'
      ? true
      : undefined,
  )

  // Joe in Headlamp, on the Pod's page.
  const hl = await timed('Headlamp sign-in with the portal session', () => openConsole(joe.page, solar.id))
  const origin = new URL(hl.url()).origin
  await hl.goto(`${origin}/c/cluster-a/pods/${solar.ns}/ticker`)

  // The log, followed: the numbers keep growing while it is open.
  await hl.getByRole('button', { name: 'Show Logs' }).click()
  const logs = hl.locator('.xterm-rows', { hasText: /tick-\d+/ })
  await timed('first log lines in Headlamp', () => expect(logs.first()).toBeVisible({ timeout: 60_000 }))
  const seen = lastTick(await logs.first().innerText())
  await timed('the log stream moves on by 3 lines', () =>
    expect.poll(async () => lastTick(await logs.first().innerText()), { timeout: 30_000 }).toBeGreaterThan(seen + 2),
  )

  // A shell in the Pod: what it computes comes back.
  await hl.getByRole('button', { name: 'Terminal / Exec' }).click()
  const terminal = hl
    .locator('.xterm')
    .filter({ hasNotText: /tick-\d+/ })
    .last()
  await terminal.waitFor({ timeout: 60_000 })
  const answer = hl.locator('.xterm-rows', { hasText: 'e2e-42' })
  await timed('a command through the Headlamp terminal', async () => {
    // The shell may still be connecting: type again until it answers.
    await until(
      'an answer in the terminal',
      async () => {
        await terminal.click()
        await hl.keyboard.type('echo e2e-$((6*7))\n')
        return (await answer.count()) > 0 ? true : undefined
      },
      60_000,
    )
  })

  // Joe logs out of Headlamp: Headlamp ends his Keycloak session too
  // (RP-initiated logout) and comes back to its own sign-in.
  await hl.getByRole('button', { name: 'Account of current user' }).click()
  await hl.getByRole('menuitem', { name: /log out/i }).click()
  const after = await timed('Headlamp logout', () =>
    until(
      'Headlamp logged out',
      async () => {
        if (await hl.locator('#kc-form-login').isVisible()) return 'keycloak form'
        if (await hl.getByRole('button', { name: /^sign in$/i }).isVisible()) return 'headlamp sign-in'
        if (new URL(hl.url()).origin !== origin) {
          const body = (
            await hl
              .locator('body')
              .innerText()
              .catch(() => '')
          )
            .replace(/\s+/g, ' ')
            .trim()
          if (/we are sorry|invalid/i.test(body)) return `keycloak error: ${body.slice(0, 120)}`
        }
        return undefined
      },
      60_000,
    ),
  )
  test.info().annotations.push({ type: 'Headlamp logout ended on', description: after })
  console.log(`Headlamp logout ended on: ${after}`)

  // Signing in again must ask who it is.
  let login = hl
  if (after !== 'keycloak form') {
    await hl.goto(origin)
    const got = await headlampSignIn(hl)
    if (got === 'silent') {
      await hl.goto(`${origin}/c/cluster-a/namespaces`)
      const back = await hl
        .getByText(solar.ns)
        .first()
        .waitFor({ timeout: 30_000 })
        .then(() => true)
        .catch(() => false)
      throw new Error(
        `after Headlamp's logout (${after}), Sign In came back without a login` +
          (back ? `, as Joe: ${solar.ns} is listed` : ''),
      )
    }
    login = got
  }

  // Bea signs in on the same browser.
  await logInAtKeycloak(login, bea.who.username, bea.who.password)
  if (login !== hl) await login.waitForEvent('close', { timeout: 10_000 }).catch(() => {}) // it may stay open
  await hl.waitForURL((u) => u.origin === origin, { timeout: 60_000 })
  await hl.goto(`${origin}/c/cluster-a/namespaces`)
  await expect(hl.getByText(wind.ns).first()).toBeVisible({ timeout: 60_000 })
  await expect(hl.getByText(solar.ns)).toHaveCount(0)
  // Headlamp names Bea, not Joe.
  const beaSub = subjectOf((await kubectlTokens(bea.page)).id_token)
  await hl.getByRole('button', { name: 'Account of current user' }).click()
  const menu = await hl.getByRole('menuitem', { name: /log out/i }).innerText()
  expect(menu).not.toContain(subjectOf(joeToken))
  const named = menu.includes(beaSub) ? 'names Bea' : `does not name Bea: ${menu.replace(/\s+/g, ' ')}`
  test.info().annotations.push({ type: 'Headlamp user menu', description: named })
  console.log(`Headlamp user menu: ${named}`)
  await hl.keyboard.press('Escape')
  // Joe's Pod is not Bea's to see.
  await hl.goto(`${origin}/c/cluster-a/pods/${solar.ns}/ticker`)
  await hl.waitForTimeout(10_000)
  await expect(hl.getByRole('button', { name: 'Show Logs' })).toHaveCount(0)
  await expect(hl.getByText(/tick-\d+/)).toHaveCount(0)

  await cleanUp()
})
