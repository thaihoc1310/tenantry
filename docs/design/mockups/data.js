// Sample data for the mockup. Shapes follow docs/architecture/02-data-model.md;
// quota numbers of a new Organization follow the onboarding policy in
// PRODUCT-PLAN.md §5.2.1. People, dates and messages are invented examples.
window.T = window.T || {};

T.GI = 'Gi';

T.me = { name: 'Alice Nguyen', email: 'alice@example.com', initials: 'AN' };
T.admin = { name: 'Platform Admin', email: 'admin@example.com', initials: 'PA' };
// Who the top bar shows on ?as=admin (Joe, OrgAdmin) and ?as=member (Bob, OrgMember).
T.joe = { name: 'Joe Tran', email: 'joe@example.com', initials: 'JT' };
T.bob = { name: 'Bob Le', email: 'bob@example.com', initials: 'BL' };
T.mallory = { name: 'Mallory', email: 'mallory@example.com', initials: 'MA' };

T.org = { id: 'org-01jabc', name: 'Acme', slug: 'acme', status: 'Active', role: 'OrgOwner', description: 'Staging environments for the Acme web team.' };

T.entitlement = { members: [3, 20], groups: [4, 10], workspaces: [3, 8] };

// The grant of a freshly approved Organization (onboarding policy).
T.freshGrant = [
  { key: 'workspaces', label: 'Workspaces', used: 0, total: 2, unit: '' },
  { key: 'namespaces', label: 'Namespaces', used: 0, total: 6, unit: '' },
  { key: 'rcpu', label: 'Shared CPU requests', used: 0, total: 2, unit: 'cores' },
  { key: 'rmem', label: 'Shared memory requests', used: 0, total: 4, unit: 'Gi' },
  { key: 'lcpu', label: 'Shared CPU limits', used: 0, total: 4, unit: 'cores' },
  { key: 'lmem', label: 'Shared memory limits', used: 0, total: 8, unit: 'Gi' },
  { key: 'pods', label: 'Shared Pods', used: 0, total: 20, unit: '' },
  { key: 'storage', label: 'Storage', used: 0, total: 20, unit: 'Gi' },
];

// The same Organization after an Admin raised the grant and added a dedicated node.
// CPU, memory and Pods count Shared Workspaces only (solar + orbit); lunar runs on a dedicated node.
T.grant = [
  { key: 'workspaces', label: 'Workspaces', used: 3, total: 4, unit: '' },
  { key: 'namespaces', label: 'Namespaces', used: 5, total: 12, unit: '' },
  { key: 'rcpu', label: 'Shared CPU requests', used: 3, total: 4, unit: 'cores' },
  { key: 'rmem', label: 'Shared memory requests', used: 6, total: 8, unit: 'Gi' },
  { key: 'lcpu', label: 'Shared CPU limits', used: 5, total: 8, unit: 'cores' },
  { key: 'lmem', label: 'Shared memory limits', used: 10, total: 16, unit: 'Gi' },
  { key: 'pods', label: 'Shared Pods', used: 30, total: 60, unit: '' },
  { key: 'storage', label: 'Storage', used: 45, total: 100, unit: 'Gi' },
];

T.cluster = { id: 'cluster-a', name: 'Cluster A', status: 'Ready', proxy: 'https://kube-a.tenantry.site', console: 'https://console-a.tenantry.site', appsDomain: 'mvp.tenantry.site', lastCheck: '20 seconds ago', version: 'v1.36.2' };

T.pool = { id: 'shared-general', nodes: 2, ratio: 3.0, cpu: { reservable: 12, reserved: 5.5 }, mem: { reservable: 24, reserved: 11 }, status: 'Available' };

T.nodes = [
  { name: 'wk3', health: 'Ready', capacity: 'PartiallyAllocated', cpu: { reservable: 7, reserved: 4 }, mem: { reservable: 14, reserved: 8 }, pods: { reservable: 100, reserved: 20 }, workspaces: ['lunar'] },
  { name: 'wk4', health: 'NotReady', capacity: 'Available', cpu: { reservable: 7, reserved: 0 }, mem: { reservable: 14, reserved: 0 }, pods: { reservable: 100, reserved: 0 }, workspaces: [], why: 'Node is not Ready. Ask a Platform Admin.' },
];

// Every node of the cluster, as the operator reports it.
T.clusterNodes = [
  { name: 'cp1', kind: 'Control plane' },
  { name: 'wk1', kind: 'System' },
  { name: 'wk2', pool: 'shared-general', cpu: 7, mem: 14 },
  { name: 'wk5', pool: 'shared-general', cpu: 7, mem: 14 },
  { name: 'wk3', org: 'Acme', cpu: 7, mem: 14 },
  { name: 'wk4', org: 'Acme', cpu: 7, mem: 14 },
  { name: 'wk6', cpu: 7, mem: 14 },
  { name: 'wk7', cpu: 7, mem: 14 },
];

T.netPool = { id: 'onboarding-pod-network', cidr: '10.240.0.0/16', prefix: 24, free: 253, total: 256 };

T.workspaces = [
  { slug: 'solar', name: 'Solar', state: 'Ready', mode: 'Shared', place: 'shared-general', cidr: '10.240.0.0/24', rcpu: 2, rmem: 4, lcpu: 4, lmem: 8, pods: 20, storage: 20, namespaces: 2, created: '18 Nov 2026' },
  { slug: 'lunar', name: 'Lunar', state: 'Ready', mode: 'Dedicated', place: 'wk3', cidr: '10.240.1.0/24', rcpu: 4, rmem: 8, lcpu: 4, lmem: 8, pods: 20, storage: 20, namespaces: 3, created: '19 Nov 2026' },
  { slug: 'orbit', name: 'Orbit', state: 'Provisioning', mode: 'Shared', place: 'shared-general', cidr: '10.240.2.0/24', rcpu: 1, rmem: 2, lcpu: 1, lmem: 2, pods: 10, storage: 5, namespaces: 0, created: '21 Nov 2026' },
];

T.conditionsReady = [
  { type: 'ClusterReady', status: 'True', message: 'Cluster A accepts changes.', when: '14:02:11' },
  { type: 'ComputeReady', status: 'True', message: 'Pods are placed on pool shared-general.', when: '14:02:12' },
  { type: 'NetworkReady', status: 'True', message: 'IP pool 10.240.0.0/24 is ready.', when: '14:02:15' },
  { type: 'CapsuleTenantReady', status: 'True', message: 'Tenant acme-solar exists.', when: '14:02:16' },
  { type: 'PolicyReady', status: 'True', message: 'Quota, limits and network policies are applied.', when: '14:02:19' },
  { type: 'IdentityBindingsReady', status: 'True', message: '3 groups are bound.', when: '14:02:20' },
  { type: 'ProxyAccessReady', status: 'True', message: 'kubectl and the web console reach this Workspace.', when: '14:02:24' },
];
T.conditionsProvisioning = [
  { type: 'ClusterReady', status: 'True', message: 'Cluster A accepts changes.', when: '16:40:03' },
  { type: 'ComputeReady', status: 'True', message: 'Pods are placed on pool shared-general.', when: '16:40:04' },
  { type: 'NetworkReady', status: 'Running', reason: 'IPPoolNotReady', message: 'Waiting for IP pool 10.240.2.0/24 to become ready.', when: 'for 2m 14s' },
  { type: 'CapsuleTenantReady', status: 'Unknown', message: 'Not started.' },
  { type: 'PolicyReady', status: 'Unknown', message: 'Not started.' },
  { type: 'IdentityBindingsReady', status: 'Unknown', message: 'Not started.' },
  { type: 'ProxyAccessReady', status: 'Unknown', message: 'Not started.' },
];
T.conditionsFailed = T.conditionsProvisioning.map((c) => c.type === 'NetworkReady'
  ? { type: 'NetworkReady', status: 'False', reason: 'IPPoolNotReady', message: 'IP pool 10.240.2.0/24 was not ready after 5 minutes.', when: '16:45:04' }
  : c);

T.members = [
  { name: 'Alice Nguyen', email: 'alice@example.com', role: 'OrgOwner', groups: ['admins'], joined: '18 Nov 2026', you: true },
  { name: 'Joe Tran', email: 'joe@example.com', role: 'OrgAdmin', groups: ['developers', 'backend'], joined: '19 Nov 2026' },
  { name: 'Bob Le', email: 'bob@example.com', role: 'OrgMember', groups: ['viewers'], joined: '19 Nov 2026' },
];
T.invitations = [
  { email: 'carol@example.com', role: 'OrgMember', group: 'developers', expires: 'in 6 days', by: 'Alice Nguyen' },
];
T.groups = [
  { name: 'admins', members: 1, sync: 'Synced', bindings: ['solar: TenantOwner', 'lunar: TenantOwner', 'orbit: TenantOwner'] },
  { name: 'developers', members: 1, sync: 'Synced', bindings: ['solar: Developer'] },
  { name: 'viewers', members: 1, sync: 'Synced', bindings: ['solar: Viewer'] },
  { name: 'backend', members: 1, sync: 'Pending', bindings: [] },
];

T.bindings = [
  { group: 'admins', role: 'TenantOwner', members: 1, sync: 'Synced' },
  { group: 'developers', role: 'Developer', members: 1, sync: 'Synced' },
  { group: 'viewers', role: 'Viewer', members: 1, sync: 'Synced' },
];
T.namespaces = [
  { name: 'acme-solar-frontend', short: 'frontend', state: 'Active', source: 'Portal', pods: 4, rcpu: '0.8', rmem: '1.5 Gi' },
  { name: 'acme-solar-backend', short: 'backend', state: 'Active', source: 'kubectl', pods: 6, rcpu: '1.0', rmem: '2.0 Gi' },
];
T.routes = [
  { name: 'web', namespace: 'frontend', service: 'web', port: 80, host: 'web-acme-solar.mvp.tenantry.site', state: 'Active' },
  { name: 'api', namespace: 'backend', service: 'api', port: 8080, host: 'api-acme-solar.mvp.tenantry.site', state: 'Creating' },
];

T.audit = [
  { when: '21 Nov 2026, 16:40', actor: 'alice@example.com', action: 'workspace.create', target: 'orbit', result: 'Accepted' },
  { when: '21 Nov 2026, 11:05', actor: 'alice@example.com', action: 'route.create', target: 'solar / api', result: 'Accepted' },
  { when: '20 Nov 2026, 09:12', actor: 'joe@example.com', action: 'group.member.add', target: 'backend / joe@example.com', result: 'Accepted' },
  { when: '19 Nov 2026, 15:30', actor: 'admin@example.com', action: 'cluster-grant.update', target: 'cluster-a', result: 'Accepted' },
  { when: '19 Nov 2026, 10:02', actor: 'alice@example.com', action: 'invitation.create', target: 'bob@example.com', result: 'Accepted' },
  { when: '18 Nov 2026, 14:20', actor: 'admin@example.com', action: 'organization.approve', target: 'acme', result: 'Accepted' },
];

T.requests = [
  { name: 'Globex', slug: 'globex', by: 'hank@example.com', purpose: 'Demo environments for a customer workshop.', at: '21 Nov 2026, 09:17' },
  { name: 'Initech', slug: 'initech', by: 'peter@example.com', purpose: 'CI preview environments, about 3 small apps.', at: '20 Nov 2026, 17:44' },
];
T.adminOrgs = [
  { name: 'Acme', slug: 'acme', owner: 'alice@example.com', workspaces: '3 of 4', cpu: '3 of 4', status: 'Active' },
  { name: 'Umbrella', slug: 'umbrella', owner: 'jill@example.com', workspaces: '1 of 2', cpu: '1 of 2', status: 'Active' },
];
T.grants = [
  { org: 'Acme', slug: 'acme', cluster: 'Cluster A', clusterId: 'cluster-a', ws: '3 of 4', cpu: '3 of 4 cores', mem: '6 of 8 Gi', pool: 'shared-general', nodes: 2 },
  { org: 'Umbrella', slug: 'umbrella', cluster: 'Cluster A', clusterId: 'cluster-a', ws: '1 of 2', cpu: '1 of 2 cores', mem: '2 of 4 Gi', pool: 'shared-general', nodes: 0 },
];
T.operations = [
  { id: 'op-01jd4f', type: 'workspace.delete', org: 'umbrella', target: 'lab', state: 'Blocked', detail: '2 PersistentVolumeClaims still have finalizers.', age: '18 min' },
  { id: 'op-01jd3z', type: 'workspace.create', org: 'acme', target: 'orbit', state: 'Running', detail: 'NetworkReady: waiting for IP pool.', age: '2 min' },
  { id: 'op-01jd2b', type: 'node.grant', org: 'acme', target: 'wk4', state: 'Failed', detail: 'Node has Pods of another tenant.', age: '1 h' },
  { id: 'op-01jd1q', type: 'route.create', org: 'acme', target: 'solar / api', state: 'Succeeded', detail: '', age: '5 h' },
];
T.health = [
  { name: 'Platform API', state: 'ok', detail: 'Answers /healthz.' },
  { name: 'Provisioning worker', state: 'ok', detail: 'Outbox lag 0 s, 0 events waiting.' },
  { name: 'Group sync worker', state: 'warn', detail: '1 group waiting for Keycloak for 4 min.' },
  { name: 'Keycloak', state: 'ok', detail: 'Realm tenantry reachable.' },
  { name: 'Cluster A', state: 'ok', detail: 'Last check 20 seconds ago.' },
];
