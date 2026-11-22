# Persona scenario, end to end

PRODUCT-PLAN §24.3, run on 2026-11-23 against the lab with `make e2e-personas` (`test/e2e`, Go). Backend
9db12ae, operator `src-fe11a06ba44e`, Capsule 0.15.2 tuned. Every step of the scenario passed. The
Headlamp SSO step needs a browser and is left to the web e2e suite (Playwright).

The personas are throwaway accounts, not the demo's: `optest-alice-*`, `optest-joe-*`, `optest-bob-*`.
Each signs up through Keycloak's own pages, with the address verified from Mailpit.
- The Platform Admin is `admin`.
- Each persona works through the portal's API as the browser does (session cookie, CSRF header,
  idempotency keys).
- Joe and Bob use kubectl and helm with their kubeconfig from the portal: its server `kube-a` (Capsule
  Proxy behind Traefik) and their own token from the `kubectl` client, which kubelogin would fetch.
- Admin kubectl checks only what no persona can see: where a Pod runs, what a node carries.

## Steps and what was checked

| Step | Result |
|---|---|
| Alice asks for an Organization; the Platform Admin approves it with the onboarding policy's grant | `admins`, `developers`, `viewers` synced to Keycloak |
| Alice invites Joe and Bob; each accepts the link from their own account | both members |
| Joe → `developers`, Bob → `viewers`; groups `backend` and `qa` | synced |
| Shared Workspace Solar: 2 CPU / 4Gi requests, 4 CPU / 8Gi limits, network auto-allocated; `admins` TenantOwner, `developers` Developer, `viewers` Viewer | operation Succeeded |
| Namespace `frontend` on the portal | operation Succeeded |
| Joe's kubeconfig | server `kube-a`, `oidc-login`, client `kubectl`; through the proxy Joe sees `<org>-solar-frontend` |
| Joe: `helm install web deploy/demo-app` | Deployment Ready, PVC on `standard` Bound |
| Alice makes the Route `web` → Service `web`:80 | Active; `https://web-<org>-solar.mvp.tenantry.site` answers 200 |
| Bob | lists Pods; refused deleting the Deployment and reading Secrets (RBAC, 403); refused making a Namespace on the portal (403) |
| Platform Admin: grant raised (maxWorkspaces 4, maxNamespacesTotal 10, requestsStorage 50Gi), wk3 out of `shared-general`, drained and uncordoned, granted to the Organization (system reserve 500m / 1536Mi / 10) | node Ready for the Organization, 2.5 CPU reservable |
| Dedicated Water and Green, 1 CPU / 4Gi each; a third with 1 CPU | Water and Green Succeeded; the third refused: "node fdp-k8s-wk3 can hold 2500m requestsCpu; its Workspaces hold 2 and this asks 1 more" (409 `DEDICATED_NODE_INSUFFICIENT_CAPACITY`) |
| CIDR and placement | Solar's Pod 10.240.0.129 in 10.240.0.0/24 on wk2 (`shared-general`); Water's Pod 10.240.1.128 in 10.240.1.0/24 on wk3 |
| Solar ↔ Water | both directions blocked (NetworkPolicy) |
| Joe in Water, where he is not bound | refused (403) |
| Alice removes Joe from the Organization | a new token lacks `developers` and is refused in Solar (403). The old token still works until it expires: the contract of architecture/06 §5 |
| Alice deletes Water | Green stays Ready. wk3: 1 CPU / 4Gi / 10 Pods reserved by Green, 1500m remaining of 2500m |
| Headlamp SSO | not run here: browser login, web e2e suite |

## Cleanup

Each cleanup is registered right after what it undoes is made, and they run in reverse order whatever
fails, each with its own time limit:
1. Green, Water and Solar are deleted.
2. wk3 is uncordoned, its grant revoked and the node returned to `shared-general`.
3. The Organization is deleted. This is tried again until the CIDR quarantine is over: a deleted
   Workspace keeps its row for 10 minutes, PRODUCT-PLAN §4.2.
4. The three accounts are disabled.

The cluster was clean 6 minutes after the last step. The rest of the run waits for the quarantine.

## What the earlier runs found

Six earlier runs failed before this one, none on a product fault:
- the test sent a `groupName` the API does not take;
- the demo chart lacked `runAsGroup`, so its init container could not write to the NFS volume (fixed in
  `deploy/demo-app`);
- the test read a created Route at the wrong level of the answer;
- the scenario's drain step was missing: the grant rightly refused wk3 while Solar's Pod still ran there
  (`NodeNotClean`);
- the portal twice did not answer in 30s; the client now retries a dropped connection.
