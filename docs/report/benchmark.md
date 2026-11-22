# Control-plane benchmark

Run on 2026-11-23 with `operator/test/isolation/bench.sh 10 20 50` (TestBenchmark, build tag `lab`).
Data: `benchmark-control-plane.csv`, `baseline-kind-vcluster.csv`.

## Setup

- Lab: kubeadm Kubernetes 1.36.2, containerd 2.2.5, etcd 3.6.8, Ubuntu 24.04.
  - Nodes:
    - `cp1`: 4 CPU, 8 GiB;
    - `wk1` (system pool): 3 CPU, 8 GiB;
    - `wk2`, `wk3` (`shared-general`): 3 CPU, 12 GiB each.
  - Capsule 0.15.2: `--workers=1`, client QPS 20, burst 30, limits 512Mi.
  - Capsule Proxy 0.15.3, Calico 3.32.0.
  - Operator `src-a8c13f9b1934`: one replica, requests 50m/64Mi, limit 256Mi.
- Each Workspace is created directly as a PlatformWorkspace, all N at once. It is small:
  - Shared on `shared-general`;
  - one Namespace;
  - quota 100m CPU / 2 Pods;
  - its own /26 network from 10.240.128.0/18, so an IPPool too;
  - one owner group.

  No workload runs: this measures the control plane only.
- How each number is measured:
  - **Ready**: from create to `status.phase=Ready`, all conditions True, ProxyAccessReady included.
  - **all ready / all deleted**: wall time until every Workspace is Ready, then until every CR is gone after deleting them all.
  - **Requests**: `rest_client_requests_total` of the operator (its HTTPS metrics, read with a throwaway scraper
    token) and of Capsule (its metrics Service), before and after each run.
  - **`apiserver_requests_all`**: the delta of the API server's `apiserver_request_total` over the run.
    That counter has no user label, so this figure also includes every other client (the backend worker's
    informers, kubelets, controllers).
  - **CPU/memory**: the peak of the operator's and Capsule's Pod metrics, sampled every 5s.
- Health: no new API server 5xx during the runs (62 before, 62 after N=50). etcd stayed at its baseline,
  2–3 "apply request took too long" warnings per 15 minutes; `readyz` was ok throughout.

## Results

| N | Ready p50 | Ready p95 | Ready max | all Ready | all deleted | operator requests | Capsule requests | API server requests (all clients) | operator CPU / mem peak | Capsule CPU / mem peak |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 (reference run) | 10–20 s | | | | | | | | | |
| 10 | 1m42s | 2m22s | 2m31s | 2m35s | 15s | 2,260 | 8,875 | 11,489 | 20m / 22Mi | 101m / 50Mi |
| 20 | 2m36s | 3m52s | 3m53s | 3m55s | 27s | 3,418 | 17,595 | 21,633 | 31m / 25Mi | 103m / 53Mi |
| 50 | 3m51s | 8m48s | 8m49s | 8m57s | 1m38s | 9,182 | 46,589 | 56,204 | 64m / 34Mi | 160m / 65Mi |

Per Workspace: the operator makes about 180–225 API requests, and Capsule about 900. Every Workspace was Ready
and was deleted cleanly, with no IPPool or IPAM block left over.

## Capsule tuned: 4 workers, client QPS 40, burst 80

Run on 2026-11-23 after `values/capsule.yaml` set `workers: 4`, `clientConnectionQPS: 40` and
`clientConnectionBurst: 80`, with requests of 100m CPU (defaults: 1, 20, 30, 50m). Same scenario.
The operator was `src-fe11a06ba44e`, which only reads Capsule's status differently (see below).

| N | Capsule | Ready p50 | Ready p95 | all Ready | all deleted | Capsule requests | Capsule CPU / mem peak |
|---|---|---|---|---|---|---|---|
| 10 | default | 1m42s | 2m22s | 2m35s | 15s | 8,875 | 101m / 50Mi |
| 10 | tuned | 1m11s | 1m47s | 1m49s | 28s | 8,154 | 191m / 48Mi |
| 20 | default | 2m36s | 3m52s | 3m55s | 27s | 17,595 | 103m / 53Mi |
| 20 | tuned | 1m17s | 2m08s | 2m15s | 25s | 15,162 | 161m / 54Mi |

- Tuning helps. At N=20 the median Workspace is Ready in half the time and the last one at 2m15s instead of
  3m55s.
- Capsule's peak CPU is under 0.2 core.
- There was no restart, and no error beyond the known replication race below.
- The tuned values are kept.
- The operator's request count was not taken in the tuned runs: the metrics port-forward was not up yet
  when the script read it. `bench.sh` now waits for it. The tuning does not change what the operator does.

## Capsule's replication race, before and after the operator's fix

Capsule 0.15.2 replicates a GlobalTenantResource into a new Namespace from two controllers at once. The
loser fails and retries a few seconds later, and Capsule shows Ready=False meanwhile. Seen on the lab, its
item errors come in three forms:
- `already exists`: both create the object;
- `the object has been modified`: both update it;
- `because it is being terminated`: it writes into a Namespace being deleted.

Draft upstream issue: `upstream-issue-capsule-replication-race.md`.

Since commit d9fb1aa, the operator reads those errors as work under way for up to 2 minutes, from the
GlobalTenantResource's condition time; any other error, or a longer wait, is still `CapsuleNotReady`.

Measured on one Workspace with a network (6 replicated objects), changing it every 12s: 8 Namespaces
added, then 4 deleted, sampled every second.

| Operator | Samples | Workspace Degraded | PolicyReady CapsuleNotReady | PolicyReady Progressing |
|---|---|---|---|---|
| before the fix (`src-54fc8d58a486`) | 144 | 65 (45%) | 65 | 0 |
| after the fix (`src-fe11a06ba44e`) | 144 | 0 | 0 | 78 |

## Baseline: one environment per tenant on the dev machine

Dev machine: 16 CPU, 15 GiB. Images were cached for all three.

| Environment | Time to ready | Idle CPU | Idle memory | Extra Pods |
|---|---|---|---|---|
| kind cluster (kind 0.32, node v1.36.1) | 33.4 s | ~100m | ~655 MiB | 9 |
| vcluster 0.37.7 in a host cluster | 24.8–26.9 s | ~40m | ~303 MiB | 2 |
| Tenantry Workspace (lab, alone) | 10–20 s | 0 | 0 | 0 (a Tenant, a Namespace, policies, an IPPool) |

## Observations

- Capsule sets the pace, not the operator. With `--workers=1` it reconciles Tenants and GlobalTenantResources
  one at a time: alone, PolicyReady takes 4s, while with 3 at once it takes up to 71s. Ready time grows
  about linearly with N. The operator stays small, at 64m CPU and 34Mi at N=50. Capsule's request count is
  about 5× the operator's.
- Under load, Capsule's replication of the LimitRange and NetworkPolicies fails transiently ("applying of
  N resources failed"). Workspaces that were already Ready then show Degraded for a while, before Capsule
  retries and they come back.
- A Workspace costs no extra Pod and no idle CPU or memory. A vcluster costs ~300 MiB and a kind cluster
  ~650 MiB, idle, per tenant.
- Fixes and tuning found by this run:
  - The operator now reads Capsule's `Reconciling` reason as work under way, not `CapsuleNotReady` (commit 0c86d71, deployed after the runs).
  - Raising Capsule's `--workers` and client QPS: done, see "Capsule tuned".
