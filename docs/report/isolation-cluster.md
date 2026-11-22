# Isolation suite, cluster side

Run on 2026-11-23 against the lab (Kubernetes 1.36.2, Capsule 0.15.2, Capsule Proxy 0.15.3 with
`ProxyClusterScoped`, Calico 3.32, operator `src-d275bf80f9a2`), with
`make operator-isolation KUBE_CONTEXT=hocbt@kubernetes` (`operator/test/isolation`, build tag `lab`).
Result: 8/8 subtests pass, 31 refusals as wanted, 0 leaks; run time about 2 minutes.

Setup: two Workspaces of two Organizations, provisioned by the operator: `optest-iso-a` (org `01isoa`,
CIDR 10.240.250.0/24) and `optest-iso-b` (org `01isob`, 10.240.251.0/24). Both are Shared on
`shared-general`, with Namespaces `web` and `api`, namespaceQuota 3, and a quota of requests.cpu 1,
requests.memory 1Gi, pods 8, requests.storage 2Gi. The tenant is an `oidc:` owner of org A,
impersonated through the API server, so every admission layer applies as it would to a token user.
The proxy side (what a Workspace member sees through Capsule Proxy) is `cluster-verify`'s
`proxy_view` check. Everything is deleted at the end.

| Threat (PRODUCT-PLAN §20.3) | Attempt | Result | Layer that refused (status) |
|---|---|---|---|
| T08 owner binds outsiders | RoleBinding to another Organization's group | refused | VAP `tenantry-tenant-rolebinding-subjects` (403) |
| | RoleBinding to `oidc:tenantry:tenant-users` | refused | VAP `tenantry-tenant-rolebinding-subjects` (403) |
| | RoleBinding to a User | refused | VAP `tenantry-tenant-rolebinding-subjects` (403) |
| | RoleBinding to another Tenant's ServiceAccount | refused | VAP `tenantry-tenant-rolebinding-subjects` (403) |
| | RoleBinding to the own Organization's group | allowed | — |
| T13/T14 read another Workspace, Nodes, PVs | get B's Namespace; list B's Pods, Secrets | refused | RBAC (403) |
| | list every Namespace, Nodes, PersistentVolumes (direct API) | refused | RBAC (403) |
| | through Capsule Proxy: Namespaces | only the own Workspace's | ProxySetting filter (`proxy_view`) |
| | through Capsule Proxy: Nodes, a Node, its status/proxy, watch, metrics | empty list / 403 | `ProxyClusterScoped` + RBAC (`proxy_view`) |
| | B lists A's Pods | refused | RBAC (403) |
| T15 cross-Workspace network | A → A, other Namespace | reaches | allowed by `tenant-allow-same-workspace` |
| | A → B Pod, B → A Pod | blocked | NetworkPolicy `tenant-default-deny` |
| | A → 169.254.169.254, node IP :6443, `kubernetes.default` :443 | blocked | NetworkPolicy (`tenant-allow-internet-egress` excepts private ranges and metadata) |
| | A → example.com:443 (DNS + Internet) | reaches | `tenant-allow-dns`, `tenant-allow-internet-egress` |
| T16 Calico annotation, node selector, toleration | owner sets the Namespace's `ipv4pools` to B's pool | value put back | Capsule managed metadata |
| | Pod with `cni.projectcalico.org/ipv4pools` or `ipAddrs` | refused | VAP `tenantry-tenant-pod-network` (403) |
| | Pod with nodeSelector `tenantry.io/compute-pool=system` | refused | PodNodeSelector (403) |
| | Pod tolerating the system taint | refused | PodTolerationRestriction (**500**, see notes) |
| | Pod with `spec.nodeName` | refused | VAP `tenantry-tenant-node-name` (403) |
| T17 privileged Pod, NodePort | privileged Pod, hostNetwork Pod | refused | Pod Security `baseline` (403) |
| | Service NodePort, LoadBalancer | refused | Capsule rule (webhook `generic.rules`, 403) |
| T18 Ingress, Certificate | Ingress for B's hostname | refused | VAP `tenantry-tenant-ingress-lockdown` (403) |
| | Certificate for `portal.tenantry.site` | refused | VAP `tenantry-tenant-ingress-lockdown` (403) |
| T19 quotas | Pod requesting 2 CPU (limits stated) over requests.cpu 1 | refused | GlobalResourceQuota (Capsule webhook, 403) |
| | container requesting 800m with no limit | refused | LimitRange `tenantry-defaults`, default limit 500m (422) |
| | PVC of 5Gi over requests.storage 2Gi | refused | GlobalResourceQuota (Capsule webhook, 403) |
| | PVC of a StorageClass outside the allow-list | refused | Capsule `pvc` webhook (403) |
| | third Namespace (quota 3) | allowed | — |
| | fourth Namespace | refused | Capsule Namespace quota (403) |
| | Namespace named with B's prefix | refused | Capsule `forceTenantPrefix` (403) |

Notes
- PodTolerationRestriction refuses a toleration outside the whitelist with HTTP 500, not 403/422.
  This is how that upstream plugin answers; the Pod is refused all the same.
- Not run: the Dedicated cases (a Pod cannot leave its node; Guaranteed QoS). They are skipped until
  the API side of M8 can make a real node grant through the portal's admin API. Node grants are
  never created by hand.
- Cases run by the API and portal suites, not here: T01–T07, T09–T12, T20, stale group token.
