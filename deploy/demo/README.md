# Demo tools

Two commands for the defence demo of PRODUCT-PLAN §28, run from the repository
root against the lab (`KUBE_CONTEXT`, default `hocbt@kubernetes`).

## `make demo-preflight`

Read only. One line per check, PASS or FAIL, and what to do about each FAIL:

- the portal, Keycloak, Capsule Proxy (`kube-a`) and Headlamp (`console-a`)
  answer over HTTPS with a certificate valid for at least 7 more days;
- the Platform API is ready, the platform Pods are Running;
- the outbox and the group sync have nothing old waiting (their metrics);
- `wk2` and `wk3` are in `shared-general` and dedicated to nobody;
- `onboarding-pod-network` has at least 4 free /24 blocks;
- no organization `acme` and none of its Workspaces are left;
- `alice`, `joe`, `bob`, `mallory` exist in Keycloak, enabled, with the demo
  password, and belong to no organization in the portal (they may have logged
  in before);
- the Platform Admin logs in to the portal.

`make demo-preflight VERIFY=1` also runs `make cluster-verify` (about 5
minutes; it makes and removes throwaway objects).

## `make demo-reset CONFIRM=acme`

Brings the lab back to where the demo starts, through the portal API as the
people would do it, and touches nothing but the organization `acme`:

1. a request `acme` still Pending is rejected (the slug comes back);
2. an Active `acme`: its owner, who must be a demo account, deletes its
   Workspaces (a delete stuck on a Pod is forced by the Platform Admin), the
   Admin takes its dedicated nodes back, and the owner deletes `acme` once
   its address blocks are out of quarantine (up to 10 minutes);
3. `wk2` and `wk3` go back into `shared-general`;
4. the onboarding policy goes back to the seed's defaults.

Accounts are never deleted. Without `CONFIRM=acme` it prints the plan and
changes nothing.

## Passwords

Both live in the Secret `keycloak-realm` of the namespace `keycloak`:
`DEMO_USER_PASSWORD` (alice, joe, bob, mallory) and `PLATFORM_ADMIN_PASSWORD`
(`admin`). The tools read them there and never print them.
