// Package controller holds the reconcilers of the four tenantry.io CRDs.
package controller

// Every right the operator has, in one place (architecture/06 §7). The
// chart's ClusterRole is generated from these markers (make operator-generate).
// No certificates: Routes use the wildcard certificate (PRODUCT-PLAN §18).

// Own CRDs: read, finalizers, status. patch on the main resource adds and
// removes finalizers; the inventory only gets its status written.
// +kubebuilder:rbac:groups=tenantry.io,resources=platformworkspaces;platformnodegrants;platformsharedpools,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=tenantry.io,resources=platformnodeinventories,verbs=get;list;watch
// +kubebuilder:rbac:groups=tenantry.io,resources=platformworkspaces/finalizers;platformnodegrants/finalizers;platformsharedpools/finalizers,verbs=update
// +kubebuilder:rbac:groups=tenantry.io,resources=platformworkspaces/status;platformnodegrants/status;platformsharedpools/status;platformnodeinventories/status,verbs=get;patch;update

// Capsule and Capsule Proxy.
// +kubebuilder:rbac:groups=capsule.clastix.io,resources=tenants;globaltenantresources;proxysettings,verbs=get;list;watch;create;update;patch;delete
// The GlobalResourceQuota Capsule makes from a Tenant's quota: status.usage.
// +kubebuilder:rbac:groups=capsule.clastix.io,resources=globalresourcequotas,verbs=get;list;watch

// Calico: IPPools through the Calico API server, so deleting one releases
// its block affinities (IMPLEMENTATION-PLAN §4.3 step 5).
// +kubebuilder:rbac:groups=projectcalico.org,resources=ippools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.projectcalico.org,resources=ipamblocks,verbs=get;list;watch

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// Services: is a Route's target there, with its port.
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// The copies Capsule makes of a Workspace's replication, read one by one
// (subreconciler.Replicas): get only, no cache of every one in the cluster.
// +kubebuilder:rbac:groups="",resources=limitranges,verbs=get
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get
// Pods: is a node clean, what still runs on it.
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// Nodes: labels and taints with keys tenantry.io/* and workspaces.tenantry.io/*
// only, held by a ValidatingAdmissionPolicy from M8 (architecture/06 §7).
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="";events.k8s.io,resources=events,verbs=create;patch
// The metrics endpoint checks a scraper's token and right.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// The token of the proxyaccess probe is a namespaced Role of the chart,
// limited to that one ServiceAccount (templates/rbac.yaml).
