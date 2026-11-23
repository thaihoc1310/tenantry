// Load test of the platform API (ADR-0021: the DB pool and the outbox under
// load). Not part of make e2e: skipped unless LOAD=dry or LOAD=full, which
// make load-test sets. The load itself is test/load/api.js, run by k6 in
// docker (the image pinned in test/load/k6-image); this spec sets up what it
// needs, watches the lab while it runs, and removes it all again.
//
//   LOAD=full  10 OrgAdmin members, 4 small Shared Workspaces, 10 -> 100
//              VUs over about 16 minutes. The direct network only: refused
//              when the portal is reached through wg0.
//   LOAD=dry   1 member, 1 Workspace, 1 VU for 30 s: the whole chain once.
//
// Stop rules: k6 stops itself when the p95 passes 5 s or errors pass 5 %
// (api.js); this spec stops k6 when Postgres client connections pass 80 % of
// max_connections, checked before the start and every 15 s. "Lab freeze":
// `docker stop` the container named in the log; the cleanup still runs.
//
// Results go to test/load/results/<time>-<mode>/: summary.json (k6),
// k6.log, metrics.jsonl (every 15 s: Postgres connections, the tenantry_*
// metrics of the API and both workers, kubectl top of their Pods). The
// members' sessions reach k6 through a file in a private temporary
// directory, removed at the end; they are never printed.
import { execFileSync, spawn } from 'node:child_process'
import { lookup } from 'node:dns/promises'
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { expect, test } from '@playwright/test'
import {
  api,
  cleanUp,
  cleanupAfterAll,
  deleteWorkspace,
  kubectl,
  member,
  onCleanup,
  succeeded,
  until,
  workspaceOrganization,
} from './support'

const mode = process.env.LOAD
const repo = path.resolve(process.cwd(), '..') // e2e.sh runs Playwright in web/

cleanupAfterAll()

// Client connections to Postgres against its max_connections.
function postgres() {
  const out = kubectl(
    '-n',
    'tenantry-system',
    'exec',
    'postgres-0',
    '--',
    'sh',
    '-c',
    `psql -U "\${POSTGRES_USER:-postgres}" -d "\${POSTGRES_DB:-tenantry}" -At -F ' ' -c ` +
      `"SELECT current_setting('max_connections'), count(*) FROM pg_stat_activity WHERE backend_type = 'client backend'"`,
  )
  const [max, used] = out.trim().split(' ').map(Number)
  return { max: max!, used: used! }
}

// The tenantry_* samples of a platform Service's /metrics, histogram buckets left out.
function scrape(service: string) {
  const text = kubectl('get', '--raw', `/api/v1/namespaces/tenantry-system/services/${service}:metrics/proxy/metrics`)
  const out: Record<string, number> = {}
  for (const line of text.split('\n')) {
    if (!line.startsWith('tenantry_') || line.includes('_bucket{')) continue
    const i = line.lastIndexOf(' ')
    out[line.slice(0, i)] = Number(line.slice(i + 1))
  }
  return out
}

const attempt = <T>(f: () => T) => {
  try {
    return f()
  } catch (e) {
    return { error: String(e instanceof Error ? e.message : e).slice(0, 200) }
  }
}

test('the platform API under load', async ({ browser }) => {
  test.skip(mode !== 'dry' && mode !== 'full', 'set LOAD=dry or LOAD=full (make load-test)')
  test.setTimeout(90 * 60_000)
  const full = mode === 'full'
  const portal = test.info().project.use.baseURL!
  const image = readFileSync(path.join(repo, 'test/load/k6-image'), 'utf8').trim()

  // The network path, and Postgres before anything is made.
  const { address } = await lookup(new URL(portal).hostname)
  const dev = /\bdev (\S+)/.exec(execFileSync('ip', ['route', 'get', address], { encoding: 'utf8' }))?.[1]
  console.log(`load ${mode}: the portal is reached through ${dev}`)
  if (full && dev === 'wg0')
    throw new Error('LOAD=full runs on the direct network only, and the portal is reached through wg0')
  const before = postgres()
  console.log(`load ${mode}: Postgres ${before.used} of ${before.max} client connections before the start`)
  if (before.used > 0.8 * before.max) throw new Error('Postgres is past 80 % of its connections already')

  // An organization of OrgAdmins (a binding's role is theirs to change),
  // a group whose role is toggled, and small Shared Workspaces.
  const workspaces = full ? 4 : 1
  const people = full ? 10 : 1
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2eload', workspaces, 5, people + 1)
  const g = await api(owner.page, 'POST', `/organizations/${slug}/groups`, { name: 'toggled' }, crypto.randomUUID())
  expect(g.status, JSON.stringify(g.body)).toBe(201)
  const toggleGroupId = g.body.id as string
  await until('group toggled in Keycloak', async () => {
    const r = await api(owner.page, 'GET', `/organizations/${slug}/groups`)
    const t = (r.body.items as { id: string; syncStatus: string }[]).find((x) => x.id === toggleGroupId)
    return t?.syncStatus === 'Synced' ? true : undefined
  })
  const members = []
  for (let i = 0; i < people; i++) members.push(await member(browser, owner.page, slug, [], 'OrgAdmin'))

  const made: { id: string; operationId: string }[] = []
  for (let i = 1; i <= workspaces; i++) {
    const r = await api(
      owner.page,
      'POST',
      `/organizations/${slug}/workspaces`,
      {
        slug: `l${i}`,
        displayName: `Load ${i}`,
        clusterId: 'cluster-a',
        compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
        resources: {
          requestsCpu: '100m',
          requestsMemory: '128Mi',
          limitsCpu: '200m',
          limitsMemory: '256Mi',
          pods: 4,
          requestsStorage: '1Gi',
          namespaceQuota: 1,
        },
        namespaces: [],
        bindings: [
          { groupId: admins.id, role: 'TenantOwner' },
          { groupId: toggleGroupId, role: 'Viewer' },
        ],
      },
      crypto.randomUUID(),
    )
    expect(r.status, JSON.stringify(r.body)).toBe(202)
    const id = r.body.workspace.id as string
    onCleanup(`workspace l${i}`, () => deleteWorkspace(owner.page, id, `l${i}`, `${slug}-l${i}`))
    made.push({ id, operationId: r.body.operation.id })
  }
  for (const w of made) await succeeded(owner.page, w.operationId)

  // The members' sessions, for k6 only.
  const data = mkdtempSync(path.join(tmpdir(), 'tenantry-load-'))
  onCleanup('the sessions file', async () => rmSync(data, { recursive: true, force: true }))
  const users = []
  for (const m of members) {
    const cookies = await m.page.context().cookies(portal)
    const csrf = cookies.find((c) => c.name === '__Host-tenantry_csrf')?.value
    expect(csrf, 'a CSRF cookie').toBeTruthy()
    users.push({ cookie: cookies.map((c) => `${c.name}=${c.value}`).join('; '), csrf: decodeURIComponent(csrf!) })
  }
  writeFileSync(
    path.join(data, 'fixture.json'),
    JSON.stringify({ portal: portal.replace(/\/$/, ''), org: slug, toggleGroupId, users, workspaces: made }),
    { mode: 0o600 },
  )

  const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
  const out = path.join(repo, 'test/load/results', `${stamp}-${mode}`)
  mkdirSync(out, { recursive: true })
  const name = `tenantry-load-${stamp.toLowerCase()}`
  onCleanup('the k6 container', async () => {
    attempt(() => execFileSync('docker', ['rm', '-f', name], { stdio: 'ignore' })) // gone already after a normal end
  })
  console.log(`load ${mode}: k6 runs as container ${name}; results in ${path.relative(repo, out)}`)

  // Every 15 s: Postgres (the 80 % rule), the platform's metrics, its Pods.
  const stopped: string[] = []
  const watch = () => {
    const pg = attempt(postgres)
    appendFileSync(
      path.join(out, 'metrics.jsonl'),
      JSON.stringify({
        t: new Date().toISOString(),
        postgres: pg,
        api: attempt(() => scrape('tenantry-api-metrics')),
        provisioning: attempt(() => scrape('tenantry-worker-provisioning-metrics')),
        groupsync: attempt(() => scrape('tenantry-worker-groupsync-metrics')),
        top: attempt(() => kubectl('-n', 'tenantry-system', 'top', 'pod', '--no-headers')),
      }) + '\n',
    )
    if ('used' in pg && pg.used > 0.8 * pg.max && !stopped.length) {
      stopped.push(`Postgres ${pg.used} of ${pg.max} client connections, past 80 %`)
      console.log(`load ${mode}: STOP, ${stopped[0]}`)
      execFileSync('docker', ['stop', name], { stdio: 'ignore' })
    }
  }

  const k6 = spawn(
    'docker',
    [
      'run',
      '--rm',
      '--name',
      name,
      '--network',
      'host',
      '--user',
      `${process.getuid!()}:${process.getgid!()}`,
      '-v',
      `${data}:/data:ro`,
      '-v',
      `${path.join(repo, 'test/load')}:/scripts:ro`,
      '-v',
      `${out}:/out`,
      '-e',
      `MODE=${mode}`,
      image,
      'run',
      '--summary-export',
      '/out/summary.json',
      '/scripts/api.js',
    ],
    { stdio: ['ignore', 'pipe', 'pipe'] },
  )
  for (const s of [k6.stdout, k6.stderr]) {
    s.on('data', (b: Buffer) => {
      process.stdout.write(b)
      appendFileSync(path.join(out, 'k6.log'), b)
    })
  }
  watch()
  const ticker = setInterval(watch, 15_000)
  const code = await new Promise<number>((resolve) => k6.on('close', (c) => resolve(c ?? -1)))
  clearInterval(ticker)
  watch()

  const summary = attempt(() => JSON.parse(readFileSync(path.join(out, 'summary.json'), 'utf8')))
  const m = (summary as { metrics?: Record<string, Record<string, number>> }).metrics ?? {}
  console.log(
    `load ${mode}: k6 exit ${code}; p95 ${m.http_req_duration?.['p(95)']?.toFixed(0)} ms, ` +
      `errors ${((m.errors?.value ?? 0) * 100).toFixed(2)} %, requests ${m.http_reqs?.count}, ` +
      `409 busy ${m.busy_409?.count ?? 0}, 429 ${m.limited_429?.count ?? 0}, 503 SERVICE_BUSY ${m.service_busy_503?.count ?? 0}`,
  )
  await cleanUp()
  expect(stopped, 'stopped for Postgres connections').toEqual([])
  expect(code, 'k6 exit code (99: a threshold was crossed)').toBe(0)
})
