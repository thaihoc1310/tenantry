// The approval form in the numbers people type (cores, Gi) and the Approval
// the API takes (Kubernetes quantities).
import type { Schemas } from '@/api/client'
import { cores, cpu, gib, memory } from '@/api/units'

export type GrantModel = {
  maxMembers: number
  maxGroups: number
  maxWorkspacesTotal: number
  clusterId: string
  ws: number
  ns: number
  storage: number
  pool: string // '' = no shared pool: dedicated nodes only
  rcpu: number
  lcpu: number
  rmem: number
  lmem: number
  pods: number
  net: string
  sc: string
  // Lists beyond the first, which the form's single choices do not show:
  // kept as they are, so editing a grant never drops one.
  extra: { pools: string[]; nets: string[]; classes: string[] }
}

// Form fields under which the API's field errors are shown.
export const fieldOf: Record<string, keyof GrantModel> = {
  maxMembers: 'maxMembers',
  maxGroups: 'maxGroups',
  maxWorkspacesTotal: 'maxWorkspacesTotal',
  clusterId: 'clusterId',
  maxWorkspaces: 'ws',
  maxNamespacesTotal: 'ns',
  requestsStorage: 'storage',
  sharedPoolIds: 'pool',
  requestsCpu: 'rcpu',
  limitsCpu: 'lcpu',
  requestsMemory: 'rmem',
  limitsMemory: 'lmem',
  pods: 'pods',
  networkPoolIds: 'net',
  allowedStorageClasses: 'sc',
}

export function emptyGrant(): GrantModel {
  return {
    maxMembers: 20,
    maxGroups: 10,
    maxWorkspacesTotal: 8,
    clusterId: '',
    ws: 0,
    ns: 0,
    storage: 0,
    pool: '',
    rcpu: 0,
    lcpu: 0,
    rmem: 0,
    lmem: 0,
    pods: 0,
    net: '',
    sc: '',
    extra: { pools: [], nets: [], classes: [] },
  }
}

export function fromApproval(a: Schemas['Approval']): GrantModel {
  return fromGrant(a.clusterGrant, a.entitlement)
}

// fromGrant is a grant in the form's numbers; the entitlement fields keep
// the defaults when none is given (editing a grant does not show them).
export function fromGrant(g: Schemas['ClusterGrantInput'], e?: Schemas['Entitlement']): GrantModel {
  const r = g.resources
  return {
    ...emptyGrant(),
    ...e,
    clusterId: g.clusterId,
    ws: g.maxWorkspaces,
    ns: g.maxNamespacesTotal,
    storage: gib(r.requestsStorage),
    pool: g.sharedPoolIds[0] ?? '',
    rcpu: cores(r.requestsCpu),
    lcpu: cores(r.limitsCpu),
    rmem: gib(r.requestsMemory),
    lmem: gib(r.limitsMemory),
    pods: r.pods,
    net: g.networkPoolIds[0] ?? '',
    sc: g.allowedStorageClasses[0] ?? '',
    extra: {
      pools: g.sharedPoolIds.slice(1),
      nets: g.networkPoolIds.slice(1),
      classes: g.allowedStorageClasses.slice(1),
    },
  }
}

// toApproval leaves the shared quota at zero without a shared pool: it
// means nothing then (PRODUCT-PLAN §5.2).
export function toApproval(m: GrantModel): Schemas['Approval'] {
  const shared = m.pool !== ''
  return {
    entitlement: { maxMembers: m.maxMembers, maxGroups: m.maxGroups, maxWorkspacesTotal: m.maxWorkspacesTotal },
    clusterGrant: {
      clusterId: m.clusterId,
      maxWorkspaces: m.ws,
      maxNamespacesTotal: m.ns,
      resources: {
        requestsCpu: cpu(shared ? m.rcpu : 0),
        limitsCpu: cpu(shared ? m.lcpu : 0),
        requestsMemory: memory(shared ? m.rmem : 0),
        limitsMemory: memory(shared ? m.lmem : 0),
        pods: shared ? m.pods : 0,
        requestsStorage: memory(m.storage),
      },
      sharedPoolIds: shared ? [m.pool, ...m.extra.pools] : [],
      networkPoolIds: m.net ? [m.net, ...m.extra.nets] : [...m.extra.nets],
      allowedStorageClasses: m.sc ? [m.sc, ...m.extra.classes] : [...m.extra.classes],
    },
  }
}
