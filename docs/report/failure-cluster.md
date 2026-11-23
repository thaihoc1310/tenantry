# Failure cases, cluster side

Run on 2026-11-23 against the lab with
`go test -tags lab -run TestFailure ./test/isolation` (`operator/test/isolation/failure_test.go`).
Operator `src-a8c13f9b1934` (force-delete as of commit dde990c), Capsule 0.15.2, Kubernetes 1.36.2.
Result: 4/4 pass. Every object was cleaned up, PersistentVolumes included, and `cluster-verify` passed
37/37 afterwards.

| Case (PRODUCT-PLAN §24.5) | What the test does | Result |
|---|---|---|
| Controller restart in the middle of provisioning | Creates a Workspace and, once its Tenant exists (phase Provisioning), deletes the operator Pod with grace 0 | Ready 1m8s after the kill (5s to 36s in earlier runs; the spread is the new Pod's start). Same Tenant UID, so the Tenant was not made again; 2 owners; exactly 2 Namespaces |
| Delete held by a stuck Pod | Workspace with a Pod carrying an extra finalizer, then deleted | `Deleting` names the held Namespace and the CR stays. Once the finalizer is removed, the delete ends: 19s in total, about 10s of it held on purpose |
| Force-delete with a stuck Pod | Same, then `tenantry.io/force-delete=true` | CR gone at once (under 1s). Events: `ForceDelete` "not waiting for Namespaces optest-fail-force-data", and `ForceDeleteLeftovers` "left Tenant optest-fail-force and Namespaces optest-fail-force-data terminating; stuck Pods: optest-fail-force-data/held". The Tenant left behind has only the operator as owner and no RoleBinding, so nobody of the tenant reaches what is left. The Namespace stays Terminating until the Pod goes |
| PVC with a foreign finalizer | Workspace with a PVC carrying an extra finalizer, then deleted | The Workspace goes in 11s: Capsule clears the PVC's finalizer, see below |

## What Capsule 0.15.2 does to a deleted tenant Namespace

- `capsule/tenants/namespace-cleanup` (`pkg/tenant/namespace_cleanup.go`) acts on a Terminating Namespace
  that has an owner reference to a Tenant. After 10s, and once its Pods are gone, it deletes every object
  left in the Namespace and clears their finalizers, except Capsule's own. There is no switch to turn it off.
  So in a tenant Namespace only a stuck Pod holds a delete. PVC finalizers, and those of backup or snapshot
  tools, are cleared. The same PVC in a non-tenant Namespace keeps the Namespace Terminating, as Kubernetes
  does.
- The Tenant's finalizer `controller.projectcapsule.dev/finalize` waits for every Namespace of the Tenant to be
  gone. So a force-delete cannot wait for the Tenant: it deletes it and goes on. A new Workspace of the same
  name then reports `CapsuleTenantReady=False`, reason `TenantTerminating`, and looks again every 30s
  until the old Tenant is gone.
- The deletion threshold `BlockedAfter` is 10 minutes on the lab. The tests check the `Deleting` message
  instead of waiting that long.

## Runbook: a delete held by a stuck Pod

1. The Workspace's `Deleting` condition, or the `ForceDeleteLeftovers` Event after a force-delete, names the
   Namespaces and the Pods.
2. Fix the Pod: remove its finalizers (`kubectl -n <ns> patch pod <pod> --type=json -p '[{"op":"remove","path":"/metadata/finalizers"}]'`),
   or `kubectl -n <ns> delete pod <pod> --grace-period=0 --force` when its node is gone.
3. Nothing else to do. Capsule finishes the Namespace and the Tenant, and the operator finishes the delete
   (step 5 waits for Calico to free the Workspace's addresses).

## "unexpected EOF" on deletes from the laptop (2026-11-23)

What was seen: last night and in the morning of 2026-11-23, every DELETE sent from the dev laptop through
`k8s-api` took about 10 s. Those past about 10.25 s ended in `unexpected EOF` and were not applied; running
them again worked. GETs, watches, `kubectl wait` and port-forwards were fine. Two cluster-verify checks
failed on it, then passed a few minutes later.

The cluster is not the cause:
- From 08:00 to 08:59, kube-apiserver served 1654 DELETEs and none took over 1 s
  (`apiserver_request_duration_seconds`). No DELETE took over 5 s in any hour since.
- From a Pod inside the cluster, a DELETE takes 16 ms on average and 64 ms at most.
- The deletes that ended in EOF never reached the API server.

| Condition | Network | When | DELETE from the laptop |
|---|---|---|---|
| 1 | home network + WireGuard `wg0` through the main VPS | last night and the morning of 2026-11-23 | about 10 s, EOF past about 10.25 s |
| 2 | company network + `wg0` still up | about 13:15 to 13:34 | 0.3 to 0.7 s, no EOF (kubectl, and curl over HTTP/1.1 and HTTP/2, with and without a body, bodies up to 64 KiB) |
| 3 | company network, direct over Wi-Fi (`wg0` removed) | from about 13:35 | 0.16 to 0.62 s, no EOF |

Likely cause, not proven: the home network's path into the tunnel (home ISP or Wi-Fi to the VPS). The tunnel and
the VPS behaved from the company network. TLS hides the HTTP method from the path, so "only DELETEs" more likely
reflects when those requests ran than what they were. Condition 1 can only be reproduced at home.

To confirm it at home, with `wg0` up:
1. Run the same sampler: a ConfigMap created, read and deleted every 20 s in a scratch namespace, with `ss -ti` on slow ones.
2. Check the path MTU through the tunnel with `ping -M do -s <size> 203.29.16.150`. A size of 1252 (a 1280-byte
   packet) must pass. Find where larger ones stop.
3. Run `tracepath 203.29.16.150`, then the same to the VPS's public address outside the tunnel.

Measured details: `eof-path.md`. cluster-verify retries a delete cut off by a dropped connection up to 3 times
(`kdelete` in `deploy/cluster/verify.sh`); a refusal still fails.

**For the user:** at the company, reach the lab directly without `wg0`. At home with `wg0`, an admin command
that ends in `unexpected EOF` was not applied: run it again.
