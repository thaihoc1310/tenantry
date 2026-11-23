# Portal end to end (Playwright)

The web suite (`web/e2e`, `make e2e`) run in full against the lab: Definition of Done item 15
(`dod-phase1.md`). Each test signs up throwaway `e2e*` accounts through Keycloak's own pages, with
the verification mail read from Mailpit. It works through the portal as a browser does, and checks
the cluster with kubectl where a test puts something there. Since fcb3afd each spec also removes its
own Organization at its end, as the Platform Admin (option C).

Two runs on 2026-11-23: an interrupted one in the afternoon, and the full one in the evening.

## Evening run: the result of record

| | |
|---|---|
| Date | 2026-11-23, 23:15–23:48 +07:00 |
| Repo | `2507adf` (feat/tenantry) |
| Images | backend `tenantry/backend:src-74a3593aa136` (API, group sync and provisioning workers), web `tenantry/web:src-df83049a9bd3`, operator `tenantry/operator:src-856c90f9001c` (rev 14, with the faster Capsule Proxy probe of `namespace-latency.md`) |
| Network | home, through the WireGuard tunnel (`wg0`, 10.66.66.2 via the main VPS). This is the path with the slow DELETEs of `eof-path.md` |
| Before | `make demo-preflight VERIFY=1`: 16 passed (cluster-verify 37/37); `make demo-purge-test-data CONFIRM=e2e` removed the afternoon's leftovers |

### Every test

The full run: 19 tests, one worker, 20.4 minutes.

| Spec | Test | Result | Duration |
|---|---|---|---:|
| `access.spec.ts` | a Developer and a Viewer reach their own Workspace only, through the proxy and the console | pass | 2.3 min |
| `access.spec.ts` | the console signs in someone in 20 groups | pass | 43.2 s |
| `access.spec.ts` | someone taken out of a group loses the portal at once and Kubernetes within 5 minutes | pass | 6.9 min |
| `admin-cluster.spec.ts` | an Admin registers a cluster, which stays Registered until made Ready, and deletes it | pass | 8.5 s |
| `admin.spec.ts` | an Admin creates an Organization and gives it a dedicated node | pass | 18.9 s |
| `dedicated.spec.ts` | Dedicated Workspaces share a node without overbooking it | pass | 2.1 min |
| `health.spec.ts` | a Platform Admin sees the platform healthy, the cluster checked | pass | 3.9 s |
| `lifecycle.spec.ts` | quota, resize, cordon and delete of a Workspace on the cluster | pass | 1.9 min |
| `network.spec.ts` | Workspaces get their own address blocks and cannot reach each other | flaky: FAIL, pass on rerun | 18.7 s / 1.3 min |
| `onboarding.spec.ts` | a newcomer gets an approved Organization | pass | 12.0 s |
| `onboarding.spec.ts` | a rejected requester sees why and may ask again | pass | 9.5 s |
| `onboarding.spec.ts` | a person without the role cannot open the admin area | pass | 4.4 s |
| `onboarding.spec.ts` | the session outlives the access token | pass (`E2E_SLOW=1`, run alone) | 5.4 min |
| `people.spec.ts` | an invited newcomer joins, gets a group, and their token names it | flaky: FAIL, pass on rerun | 31.2 s / 34.3 s |
| `people.spec.ts` | ownership changes hands when the recipient accepts | flaky: FAIL, pass on rerun | 42.4 s / 19.8 s |
| `routes.spec.ts` | a Route publishes a Service over HTTPS and goes with its Namespace | flaky: FAIL, pass on rerun | 6.9 s / 46.0 s |
| `wizard.spec.ts` | the wizard creates a Shared Workspace that reaches Ready | flaky: FAIL, pass on rerun | 1.7 min / 1.1 min |
| `wizard.spec.ts` | the wizard explains what stops a Workspace | pass | 43.2 s |
| `workspace.spec.ts` | a Workspace reaches the cluster with its owners and Namespaces | pass | 40.1 s |

Totals:
- Full run: 13 passed, 5 failed, 1 skipped (the slow test, which needs `E2E_SLOW=1`).
- Rerun of the 5 failures, once, with `--last-failed`: 5 passed (4.2 min).
- `E2E_SLOW=1` for the slow test alone: 1 passed (5.4 min).
- In all, all 19 tests passed, 5 of them only on a rerun.

### The five failures, and why

**Four come from the laptop's network.** Between 23:28 and 23:32 local, Chromium logged
`net::ERR_NETWORK_CHANGED` on requests to the portal, Keycloak and the console. The laptop's network
changed under the tunnel several times in those minutes. Nothing failed on the lab side.

| Test | What failed | In the trace |
|---|---|---|
| network | after Register, "verify your email" never showed | the registration POST to Keycloak: `ERR_NETWORK_CHANGED` |
| people: invited newcomer | the portal page stayed blank, so no Register link within 30 s | the portal's main script `index-*.js`: `ERR_NETWORK_CHANGED` |
| people: ownership | the same, on the next sign-up | Keycloak's login page assets and `/auth/login`: `ERR_NETWORK_CHANGED` |
| routes | creating the Organization: 422 `OWNER_USER_NOT_FOUND` | the owner's sign-up lost its Keycloak page assets (`ERR_NETWORK_CHANGED`), so the owner never reached the portal |

**One is slowness on the cluster side**, with no network error in its trace. In "the wizard creates
a Shared Workspace", the new Namespace existed in kubectl at once. Its `namespace.create` operation,
however, stayed Running for about 67 s (its event stream 23:33:11–23:34:18). The test waits 60 s for
"Create namespace" to be enabled again, and that button stays disabled while an operation runs. On
the rerun the same step finished within the budget. B's measurement (`namespace-latency.md`) puts
the user-side p95 at 13.8 s with operator rev 14; this one was an outlier past the test's 60 s.

### What the run logged

- Mailpit port-forward reopened by `e2e.sh` (db98e20, 2507adf): 0 times, in all three runs.
- Retries after an EOF or timeout: none inside the Playwright suite (it has no retry layer of its
  own). Outside it, before the run, one `GET /operations/{id}` of the admin Workspace delete hit "The
  read operation timed out" and was retried once by `deploy/demo/lab.py`.
- Option C, Organizations left because their address blocks were in quarantine (the cleanup does not
  wait 10 minutes; `make demo-purge-test-data` takes them afterwards):

```
organization e2eactgwfsh left until 2026-11-23T16:27:56Z: its address blocks are in quarantine
organization e2egrtgzdqm left until 2026-11-23T16:28:38Z: its address blocks are in quarantine
organization e2ervth0b45 left until 2026-11-23T16:35:28Z: its address blocks are in quarantine
organization e2ednth9pj7 left until 2026-11-23T16:37:58Z: its address blocks are in quarantine
organization e2elcthchy6 left until 2026-11-23T16:40:04Z: its address blocks are in quarantine
organization e2ewzthi6fc left until 2026-11-23T16:44:30Z: its address blocks are in quarantine
organization e2ewsthlrbw left until 2026-11-23T16:45:59Z: its address blocks are in quarantine
organization e2enwthosgz left until 2026-11-23T16:48:57Z: its address blocks are in quarantine (rerun)
organization e2ertthrqfh left until 2026-11-23T16:50:45Z: its address blocks are in quarantine (rerun)
organization e2ewzthsqcu left until 2026-11-23T16:51:52Z: its address blocks are in quarantine (rerun)
```

- Organizations without Workspaces were deleted at once (onboarding, people, admin, admin-cluster's
  cluster): no line.

## Afternoon run: interrupted, and what it taught

| | |
|---|---|
| Date | 2026-11-23, 14:44–15:02 +07:00 |
| Images | backend `src-582a475972a2`, web `src-dd50cdab9b53`, operator `src-856c90f9001c` |
| Network | company, direct (`wlo1`) |
| Tests | 18 (`admin-cluster.spec` left out: its endpoint was not deployed yet) |

Results:
- 6 passed: access ×3, admin, dedicated, health.
- 11 failed: lifecycle, network, onboarding ×3, people ×2, routes, wizard ×2, workspace.
- 1 skipped: the slow test.

Every failure was the same: `no verification mail for e2e…@tenantry.site in Mailpit`, one after
another from 14:56:04 to 15:02:06. `e2e.sh` opened the port-forward to Mailpit once. It dropped at
about 14:55, and from then on every sign-up waited in vain for its verification mail. The portal and
Keycloak were fine: the sign-ups reached Keycloak's registration page. The fix, db98e20, reopens the
port-forward whenever it drops, and 2507adf logs each reopen.

Two side notes from that afternoon:
- **An aborted start.** A first start at 14:41, stopped after 51 s, left the Organization
  `e2eacsyiwjb` with a live Workspace: its cleanup never ran. The owner's password is random, and no
  Platform Admin path existed to delete a tenant's Workspace. 4d11107 added one (`DELETE
  /workspaces/{id}` for a Platform Admin, through the owner's flow and confirmName, audited as the
  admin's). Before the evening run it removed `e2eacsyiwjb-solar` (operation Succeeded). The
  Organization itself goes with the purge once its address block leaves quarantine.
- **A script error on Keycloak's registration page.** Each sign-up's trace holds the page error
  `TypeError: Cannot read properties of null (reading 'addEventListener')`. It comes from Keycloak
  26.8's stock `keycloak.v2` register template: an inline script calls
  `document.getElementById("password").addEventListener(…)` for its password-policy hints. Our realm
  verifies email before a password is set (`verifyEmail: true`), so the form has no `password`
  field. We have no theme of our own (`deploy/keycloak` holds only the realm). The error is harmless,
  since the registration works in every passing test, and upstream.

## Platform Admin password rotated

On 2026-11-23, after the evening run, the Platform Admin password (`PLATFORM_ADMIN_PASSWORD` in the
Secret `keycloak-realm`) was rotated: it had reached a working transcript through the action titles
of a kept Playwright trace. The traces were deleted. Since then the suite types passwords without
recording them (`fillSecret`, and traces without DOM snapshots: `web/e2e/trace.ts`, checked by
`trace-secrets.spec.ts`).

## How to repeat

```sh
make demo-preflight VERIFY=1         # 16 passed
make demo-purge-test-data            # a plan; CONFIRM=e2e to remove earlier leftovers
make e2e                             # 19 tests, the slow one skipped
deploy/cluster/e2e.sh --last-failed  # a test that failed on a network step, once more
E2E_SLOW=1 deploy/cluster/e2e.sh -g 'outlives the access token'   # the 5-minute session test
make demo-purge-test-data            # after the Organizations' quarantine (10 min): their leftovers
```

A test that fails on a network step is rerun once, and both results are written down; it is not
hidden.
