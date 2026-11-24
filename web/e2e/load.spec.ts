// Load test of the platform API (ADR-0021: the DB pool and the outbox under
// load). Not part of make e2e: skipped unless LOAD=dry or LOAD=full, which
// make load-test sets. The load itself is test/load/api.js, run by k6 in
// docker (the image pinned in test/load/k6-image); this spec sets up what it
// needs, watches the lab while it runs, and removes it all again.
//
//   LOAD=full  10 OrgAdmin members, 4 small Shared Workspaces, 10 -> 100
//              VUs over about 16 minutes.
//   LOAD=dry   1 member, 1 Workspace, 1 VU for 30 s: the whole chain once.
//
// Before either, the probe (api.js MODE=probe) checks the network path k6
// takes: 10 rounds of an invitation created, listed and revoked. An EOF or a
// DELETE over 3 s stops the run there (docs/report/eof-path.md: from home
// through wg0, such DELETEs took 10 s and ended in EOF). network.json keeps
// the interface, the path (wg0 or direct) and the probe's numbers.
//
// Stop rules: k6 stops itself when the p95 passes 5 s or errors pass 5 %
// (api.js); this spec stops k6 when Postgres client connections pass 80 % of
// max_connections, checked before the start and every 15 s. "Lab freeze":
// `docker stop` the container named in the log; the cleanup still runs.
//
// Results go to test/load/results/<time>-<mode>/: summary.json (k6),
// k6.log, metrics.jsonl (every 15 s: Postgres connections, the tenantry_*
// metrics of the API and both workers, kubectl top of their Pods), server.json
// (the API's own p95 per route over the run, from its histogram: k6 measures
// at the client, across the WAN; the server's time is what is left). The
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

// The tenantry_* samples of a platform Service's /metrics; histogram buckets
// left out but for the API's request durations.
function scrape(service: string) {
  const text = kubectl('get', '--raw', `/api/v1/namespaces/tenantry-system/services/${service}:metrics/proxy/metrics`)
  const out: Record<string, number> = {}
  for (const line of text.split('\n')) {
    if (!line.startsWith('tenantry_')) continue
    if (line.includes('_bucket{') && !line.startsWith(`${DURATION}_bucket{`)) continue
    const i = line.lastIndexOf(' ')
    out[line.slice(0, i)] = Number(line.slice(i + 1))
  }
  return out
}

const DURATION = 'tenantry_api_request_duration_seconds'

// The API's p95 per route and method between two scrapes, interpolated in
// its buckets as Prometheus' histogram_quantile does; past the last bucket
// it says ">5000".
function serverP95(first: Record<string, number>, last: Record<string, number>) {
  const routes = new Map<string, [number, number][]>()
  for (const [key, v] of Object.entries(last)) {
    const m = new RegExp(`^${DURATION}_bucket\\{(.*),le="([^"]+)"\\}$`).exec(key)
    if (!m) continue
    const le = m[2] === '+Inf' ? Infinity : Number(m[2])
    const route = m[1]!.replace(/(\w+)="([^"]*)"/g, '$1=$2')
    routes.set(route, [...(routes.get(route) ?? []), [le, v - (first[key] ?? 0)]])
  }
  const out: Record<string, { requests: number; p95ms: number | string }> = {}
  for (const [route, buckets] of routes) {
    buckets.sort((a, b) => a[0] - b[0])
    const total = buckets.at(-1)![1]
    if (!total) continue
    const rank = 0.95 * total
    let [prevLe, prevN] = [0, 0]
    for (const [le, n] of buckets) {
      if (n >= rank) {
        out[route] = {
          requests: total,
          p95ms:
            le === Infinity
              ? `>${prevLe * 1000}`
              : Math.round((prevLe + ((le - prevLe) * (rank - prevN)) / (n - prevN || 1)) * 1000),
        }
        break
      }
      ;[prevLe, prevN] = [le, n]
    }
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
  const via = dev === 'wg0' ? 'wg0' : 'direct'
  console.log(`load ${mode}: the portal is reached through ${dev} (${via})`)
  const before = postgres()
  console.log(`load ${mode}: Postgres ${before.used} of ${before.max} client connections before the start`)
  if (before.used > 0.8 * before.max) throw new Error('Postgres is past 80 % of its connections already')

  // An organization of OrgAdmins (a binding's role is theirs to change),
  // a group whose role is toggled, and small Shared Workspaces.
  const workspaces = full ? 4 : 1
  const people = full ? 10 : 1
  const { owner, slug, admins } = await workspaceOrganization(browser, 'e2eload', workspaces, 5, people + 2) // + room for the probe's invitations
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
  console.log(`load ${mode}: k6 runs as container ${name}; results in ${path.relative(repo, out)}`)

  // k6 in docker, its output in <dir>/k6.log and on the console.
  const k6run = async (k6mode: string, dir: string, container: string) => {
    onCleanup(`the k6 container ${container}`, async () => {
      attempt(() => execFileSync('docker', ['rm', '-f', container], { stdio: 'ignore' })) // gone already after a normal end
    })
    const k6 = spawn(
      'docker',
      [
        'run',
        '--rm',
        '--name',
        container,
        '--network',
        'host',
        '--user',
        `${process.getuid!()}:${process.getgid!()}`,
        '-v',
        `${data}:/data:ro`,
        '-v',
        `${path.join(repo, 'test/load')}:/scripts:ro`,
        '-v',
        `${dir}:/out`,
        '-e',
        `MODE=${k6mode}`,
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
        appendFileSync(path.join(dir, 'k6.log'), b)
      })
    }
    return k6
  }
  const exit = (k6: ReturnType<typeof spawn>) =>
    new Promise<number>((resolve) => k6.on('close', (c) => resolve(c ?? -1)))
  const metricsOf = (dir: string) =>
    (
      attempt(() => JSON.parse(readFileSync(path.join(dir, 'summary.json'), 'utf8'))) as {
        metrics?: Record<string, Record<string, number>>
      }
    ).metrics ?? {}

  // The probe, through the same path as the load.
  const probeDir = path.join(out, 'probe')
  mkdirSync(probeDir)
  const probeCode = await exit(await k6run('probe', probeDir, `${name}-probe`))
  const pm = metricsOf(probeDir)
  const del = pm['http_req_duration{name:DELETE /invitations/{id}}'] ?? {}
  const failed = Math.round((pm.http_req_failed?.value ?? 0) * (pm.http_reqs?.count ?? 0))
  const eof =
    (attempt(() => readFileSync(path.join(probeDir, 'k6.log'), 'utf8')) as string).toString().match(/EOF/g)?.length ?? 0
  const probe = {
    exit: probeCode,
    requests: pm.http_reqs?.count ?? 0,
    failed,
    eofInLog: eof,
    p95ms: Math.round(pm.http_req_duration?.['p(95)'] ?? 0),
    deleteMaxMs: Math.round(del.max ?? 0),
    deleteMedMs: Math.round(del.med ?? 0),
  }
  writeFileSync(path.join(out, 'network.json'), JSON.stringify({ interface: dev, path: via, probe }, null, 2) + '\n')
  console.log(`load ${mode}: probe ${JSON.stringify(probe)}`)
  if (probeCode !== 0) {
    const why = [
      failed && `${failed} failed requests${eof ? ' (EOF in the log)' : ''}`,
      probe.deleteMaxMs > 3000 && `a DELETE took ${probe.deleteMaxMs} ms`,
    ].filter(Boolean)
    await cleanUp()
    throw new Error(`the probe failed, so no load: ${why.join(', ') || `k6 exit ${probeCode}`}`)
  }

  // Every 15 s: Postgres (the 80 % rule), the platform's metrics, its Pods.
  const stopped: string[] = []
  const watch = () => {
    const pg = attempt(postgres)
    const api = attempt(() => scrape('tenantry-api-metrics'))
    appendFileSync(
      path.join(out, 'metrics.jsonl'),
      JSON.stringify({
        t: new Date().toISOString(),
        postgres: pg,
        api,
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
    return api
  }

  const scrapes: Record<string, number>[] = []
  const watchApi = () => {
    const api = watch()
    if (!('error' in api)) scrapes.push(api as Record<string, number>)
  }
  watchApi() // before k6's first request: it starts in about a second
  const k6 = await k6run(mode!, out, name)
  const ticker = setInterval(watchApi, 15_000)
  const code = await exit(k6)
  clearInterval(ticker)
  watchApi()

  const m = metricsOf(out)
  console.log(
    `load ${mode}: k6 exit ${code}; p95 ${m.http_req_duration?.['p(95)']?.toFixed(0)} ms, ` +
      `errors ${((m.errors?.value ?? 0) * 100).toFixed(2)} %, requests ${m.http_reqs?.count}, ` +
      `409 busy ${m.busy_409?.count ?? 0}, 429 ${m.limited_429?.count ?? 0}, 503 SERVICE_BUSY ${m.service_busy_503?.count ?? 0}`,
  )
  for (const [key, v] of Object.entries(m)) {
    const name = /^http_req_duration\{name:(.*)\}$/.exec(key)?.[1]
    if (name && v.count !== 0 && v['p(95)'] !== undefined)
      console.log(`  ${name}: p95 ${v['p(95)'].toFixed(0)} ms, max ${v.max?.toFixed(0)} ms`)
  }
  const server = scrapes.length > 1 ? serverP95(scrapes[0]!, scrapes.at(-1)!) : {}
  writeFileSync(path.join(out, 'server.json'), JSON.stringify(server, null, 2) + '\n')
  console.log(`load ${mode}: the API's own p95 (server side, from its histogram):`)
  for (const [route, v] of Object.entries(server)) console.log(`  ${route}: ${v.requests} requests, p95 ${v.p95ms} ms`)
  await cleanUp()
  expect(stopped, 'stopped for Postgres connections').toEqual([])
  expect(code, 'k6 exit code (99: a threshold was crossed)').toBe(0)
})
