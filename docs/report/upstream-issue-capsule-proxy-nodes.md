Title: Node list/get ignores proxySettings: every Tenant member reads the Nodes of the Tenant's nodeSelector with the proxy's own rights

Version: capsule-proxy v0.15.3 (chart 0.15.3), Capsule 0.15.2, Kubernetes 1.36.2, feature gate ProxyClusterScoped off (default).

What happens
- internal/modules/node/list.go:57 and internal/modules/node/get.go:58 (Handle) build a selector from
  Tenant.spec.nodeSelector (utils.GetNodeSelectors) for every proxy tenant of the caller, without calling
  ProxyTenant.RequestAllowed(req, NodesProxy). The other legacy modules do check it (e.g.
  internal/modules/storageclass/utils.go:26), and defaultProxySettings() denies List for Nodes.
- The filtered request is then served with the proxy's ServiceAccount, so the caller's RBAC does not apply.
- Result: any Tenant owner, and any subject of a namespaced ProxySetting in a Tenant namespace, can
  list and get the full Node objects selected by the Tenant's nodeSelector (labels, addresses,
  status.images of every workload cached on a shared node), with no proxySettings granting Nodes.

Reproduce
1. Tenant with spec.nodeSelector {pool: shared}; two nodes labelled pool=shared.
2. ServiceAccount sa-probe (no RBAC at all), member of a capsule user group.
3. ProxySetting in a Tenant namespace with subjects [{kind: ServiceAccount, name: system:serviceaccount:<ns>:sa-probe}],
   no proxySettings.
4. TOKEN=$(kubectl create token sa-probe -n <ns>); curl -H "Authorization: Bearer $TOKEN" https://<proxy>/api/v1/nodes
   → both nodes, full objects. GET /api/v1/nodes/<name> also works.
   Expected: no Node (List not granted for Nodes), as for StorageClasses ("not allowed").

Workaround: --feature-gates=ProxyClusterScoped=true (legacy modules not registered; Node list empty,
Node get forwarded with impersonation → caller's RBAC → 403).
