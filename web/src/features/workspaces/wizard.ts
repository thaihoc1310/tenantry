// The Create Workspace wizard's state (IMPLEMENTATION-PLAN §6.4): one
// reactive form, the step it is on, the errors of each step and the move
// between steps. Leaving a step forward runs POST .../workspaces:validate
// with the whole form, so the server's own checks (quota, pool, node,
// network, groups) land on the field they are about. No rendering here:
// Vitest drives it with a fake validate.
import { computed, reactive, ref, watch } from 'vue'
import { ApiError, type ErrorCode, type Schemas } from '@/api/client'
import { cores, cpu, gib, memory } from '@/api/units'
import { errorText } from '@/i18n/errors'

export const steps = ['Name', 'Cluster and compute', 'Resources', 'Network', 'Namespaces', 'Access', 'Review'] as const
const review = steps.length - 1

export type WizardForm = {
  name: string
  slug: string
  slugEdited: boolean
  clusterId: string
  mode: 'Shared' | 'Dedicated'
  sharedPoolId: string
  nodeGrantId: string
  rcpu: number
  lcpu: number
  rmem: number
  lmem: number
  pods: number
  storage: number
  netAuto: boolean
  networkPoolId: string
  prefix: number // 0: the pool's default
  cidr: string
  nsQuota: number
  namespaces: string[]
  owners: string[] // group ids, one role each
  devs: string[]
  viewers: string[]
}

export function emptyForm(): WizardForm {
  return {
    name: '',
    slug: '',
    slugEdited: false,
    clusterId: '',
    mode: 'Shared',
    sharedPoolId: '',
    nodeGrantId: '',
    rcpu: 1,
    lcpu: 2,
    rmem: 2,
    lmem: 4,
    pods: 10,
    storage: 5,
    netAuto: true,
    networkPoolId: '',
    prefix: 0,
    cidr: '',
    nsQuota: 3,
    namespaces: [],
    owners: [],
    devs: [],
    viewers: [],
  }
}

// What the wizard can choose from, as the catalog endpoints answer.
export type Options = {
  clusters: Schemas['OrgCluster'][]
  computeOptions?: Schemas['ComputeOptions']['items']
  sharedPools?: Schemas['OrgSharedPool'][]
  nodes?: Schemas['OrgDedicatedNode'][]
  networkPools?: Schemas['OrgNetworkPool'][]
  groups?: Schemas['Group'][]
}

// A node a Dedicated Workspace can go on: Ready and not full.
export const nodeUsable = (n: Schemas['OrgDedicatedNode']) =>
  n.nodeState === 'Ready' && n.capacityState !== 'FullyAllocated' && n.capacityState !== 'Overcommitted'

// fill picks what the person has not chosen yet and has only one sensible
// answer (PRODUCT-PLAN §19.1): the default Ready cluster, the first shared
// pool, the first usable node, the first network pool, and the groups named
// admins and developers as owners and developers (an approved organization
// starts with admins, developers and viewers). Choices already made stay.
export function fill(f: WizardForm, o: Options): void {
  if (!f.clusterId) {
    const c =
      o.clusters.find((c) => c.isDefault && c.status === 'Ready') ?? o.clusters.find((c) => c.status === 'Ready')
    if (c) f.clusterId = c.id
  }
  const dedicatedOnly = o.computeOptions?.every((m) => m.mode === 'Dedicated' || !m.available)
  if (dedicatedOnly && o.computeOptions?.some((m) => m.mode === 'Dedicated' && m.available)) f.mode = 'Dedicated'
  if (!f.sharedPoolId && o.sharedPools?.length) f.sharedPoolId = o.sharedPools[0]!.id
  if (!f.nodeGrantId) {
    const n = o.nodes?.find(nodeUsable)
    if (n) f.nodeGrantId = n.id
  }
  if (!f.networkPoolId && o.networkPools?.length) f.networkPoolId = o.networkPools[0]!.id
  if (o.groups && !f.owners.length && !f.devs.length && !f.viewers.length) {
    const synced = (name: string) =>
      o.groups!.filter((g) => g.name === name && g.syncStatus === 'Synced').map((g) => g.id)
    f.owners = synced('admins')
    f.devs = synced('developers')
  }
}

// slugOf turns a display name into a slug the API takes: lowercase letters,
// digits and single hyphens, starting with a letter, at most 20.
export function slugOf(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^[^a-z]+/, '')
    .slice(0, 20)
    .replace(/-+$/, '')
}

export const slugPattern = /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/
const suffixPattern = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/

// toCreate is the request body. Dedicated: limits equal the requests and are
// left out; Auto network: only the pool, the server picks the block.
export function toCreate(f: WizardForm): Schemas['WorkspaceCreate'] {
  const dedicated = f.mode === 'Dedicated'
  const role = (ids: string[], r: Schemas['WorkspaceRole']) => ids.map((groupId) => ({ groupId, role: r }))
  return {
    slug: f.slug,
    displayName: f.name.trim(),
    clusterId: f.clusterId,
    compute: dedicated
      ? { mode: 'Dedicated', dedicatedNodeGrantId: f.nodeGrantId }
      : { mode: 'Shared', sharedPoolId: f.sharedPoolId },
    resources: {
      requestsCpu: cpu(f.rcpu),
      requestsMemory: memory(f.rmem),
      ...(dedicated ? {} : { limitsCpu: cpu(f.lcpu), limitsMemory: memory(f.lmem) }),
      pods: f.pods,
      requestsStorage: memory(f.storage),
      namespaceQuota: f.nsQuota,
    },
    namespaces: f.namespaces,
    network: {
      ...(f.networkPoolId ? { networkPoolId: f.networkPoolId } : {}),
      ...(!f.netAuto && f.prefix ? { prefixLength: f.prefix } : {}),
      ...(!f.netAuto && f.cidr.trim() ? { requestedCidr: f.cidr.trim() } : {}),
    },
    bindings: [...role(f.owners, 'TenantOwner'), ...role(f.devs, 'Developer'), ...role(f.viewers, 'Viewer')],
  }
}

// Errors are keyed by the request's field path, as the API names it
// (details.field); '' holds what no field of the wizard can fix.
export type Errors = Record<string, string>

// stepOf is the step a field is edited on; -1 for ''.
export function stepOf(field: string): number {
  const under = (p: string) => field === p || field.startsWith(p + '.')
  if (under('slug') || under('displayName')) return 0
  if (under('clusterId') || under('compute')) return 1
  if (under('resources.namespaceQuota') || under('namespaces')) return 4
  if (under('resources')) return 2
  if (under('network')) return 3
  if (under('bindings')) return 5
  return -1
}

// localErrors are the checks the form makes before asking the server: the
// API's own patterns and bounds, said in words.
export function localErrors(f: WizardForm, orgSlug: string): Errors {
  const e: Errors = {}
  if (!f.name.trim()) e.displayName = 'Give the Workspace a name.'
  if (f.slug.length < 2 || f.slug.length > 20 || !slugPattern.test(f.slug))
    e.slug = 'Use 2 to 20 lowercase letters, digits or single hyphens, starting with a letter.'
  if (!f.clusterId) e.clusterId = 'No Ready cluster is granted to the Organization.'
  if (f.mode === 'Shared' && !f.sharedPoolId) e['compute.sharedPoolId'] = 'The grant lists no shared pool.'
  if (f.mode === 'Dedicated' && !f.nodeGrantId) e['compute.dedicatedNodeGrantId'] = 'Choose a node.'
  if (!(f.rcpu > 0)) e['resources.requestsCpu'] = 'Ask for some CPU.'
  if (!(f.rmem > 0)) e['resources.requestsMemory'] = 'Ask for some memory.'
  if (f.mode === 'Shared') {
    if (f.lcpu < f.rcpu) e['resources.limitsCpu'] = 'A limit cannot be lower than its request.'
    if (f.lmem < f.rmem) e['resources.limitsMemory'] = 'A limit cannot be lower than its request.'
  }
  if (!(f.pods >= 1)) e['resources.pods'] = 'At least one Pod.'
  if (!f.netAuto && f.cidr.trim() && !/^\d{1,3}(\.\d{1,3}){3}\/\d{1,2}$/.test(f.cidr.trim()))
    e['network.requestedCidr'] = 'Write a block such as 10.240.8.0/24, or leave it empty.'
  if (!(f.nsQuota >= 1)) e['resources.namespaceQuota'] = 'At least one namespace.'
  else if (f.nsQuota < f.namespaces.length)
    e['resources.namespaceQuota'] = 'Lower than the number of namespaces you are creating below.'
  const prefix = `${orgSlug}-${f.slug}-`
  const bad = f.namespaces.find((n) => !suffixPattern.test(n) || prefix.length + n.length > 63)
  if (bad) e.namespaces = `“${bad}”: lowercase letters, digits and hyphens, and ${prefix}${bad} must fit 63 characters.`
  if (!f.owners.length) e.bindings = 'Choose at least one owner group. (LAST_OWNER_BINDING)'
  else {
    const all = [...f.owners, ...f.devs, ...f.viewers]
    if (new Set(all).size !== all.length) e.bindings = 'A group can hold only one role in the Workspace.'
  }
  return e
}

const unitOf: Record<string, [(q: string) => number, string]> = {
  requestsCpu: [cores, 'cores'],
  limitsCpu: [cores, 'cores'],
  requestsMemory: [gib, 'Gi'],
  limitsMemory: [gib, 'Gi'],
  requestsStorage: [gib, 'Gi'],
}
// left says how much of a resource is left, from two quantities of a refusal.
function left(resource: string, total: unknown, used: unknown): string {
  const [f, unit] = unitOf[resource] ?? [Number, '']
  const n = Math.max(0, Math.round((f(String(total)) - f(String(used))) * 100) / 100)
  return unit ? `${n} ${unit}` : String(n)
}

// fromError places a refusal of the server: on the field it names, else on
// the field its code is about, else on ''. The words say what is left where
// the details tell it.
export function fromError(err: ApiError): [string, string] {
  const d = err.details
  const code = err.code as ErrorCode
  const resource = typeof d.resource === 'string' ? d.resource : ''
  const byResource =
    resource === 'namespaces' ? 'resources.namespaceQuota' : resource === 'workspaces' ? '' : `resources.${resource}`
  const byCode: Partial<Record<ErrorCode, string>> = {
    CLUSTER_NOT_GRANTED: 'clusterId',
    CLUSTER_NOT_READY: 'clusterId',
    DEDICATED_NODE_NOT_READY: 'compute.dedicatedNodeGrantId',
    NETWORK_POOL_EXHAUSTED: 'network.prefixLength',
    GROUP_SYNC_PENDING: 'bindings',
    LAST_OWNER_BINDING: 'bindings',
  }
  const field = err.field || (resource ? byResource : (byCode[code] ?? ''))
  let words: string
  if (code === 'CLUSTER_GRANT_QUOTA_EXCEEDED' && resource) {
    words = `Only ${left(resource, d.limit, d.allocated)} left in the Organization's grant. Lower this, or ask a Platform Admin for more.`
  } else if (code === 'COMPUTE_POOL_CAPACITY_EXCEEDED' && resource) {
    words = `Only ${left(resource, d.limit, d.allocated)} left in the shared pool.`
  } else if (code === 'DEDICATED_NODE_INSUFFICIENT_CAPACITY' && resource) {
    words = `Only ${left(resource, d.reservable, d.reserved)} left on node ${String(d.node ?? '')}.`
  } else if (code === 'VALIDATION_FAILED') {
    words = err.message
  } else {
    words = errorText[code]?.en ?? err.message
  }
  return [field, `${words} (${code})`]
}

// useWorkspaceWizard holds one wizard. validate is POST .../workspaces:validate.
export function useWorkspaceWizard(
  orgSlug: () => string,
  validate: (body: Schemas['WorkspaceCreate']) => Promise<Schemas['WorkspaceValidation']>,
) {
  const form = reactive(emptyForm())
  const step = ref(0)
  const visited = reactive(steps.map((_, i) => i === 0))
  const server = ref<Errors>({})
  const checking = ref(false)
  // The block the Workspace would get now, from the last validation.
  const block = ref<Schemas['WorkspaceValidation']['network']>()

  // The slug follows the name until it is edited by hand.
  watch(
    () => form.name,
    (n) => {
      if (!form.slugEdited) form.slug = slugOf(n)
    },
  )
  // Dedicated: limits are the requests (Guaranteed QoS).
  watch(
    () => [form.mode, form.rcpu, form.rmem] as const,
    ([mode]) => {
      if (mode === 'Dedicated') Object.assign(form, { lcpu: form.rcpu, lmem: form.rmem })
    },
  )
  // An answer of the server is about the form it saw; any edit makes it stale.
  watch(
    () => JSON.stringify(toCreate(form)),
    () => {
      server.value = {}
      block.value = undefined
    },
  )

  const local = computed(() => localErrors(form, orgSlug()))
  const errors = computed<Errors>(() => ({ ...server.value, ...local.value }))
  const hasError = (i: number) => Object.keys(errors.value).some((k) => stepOf(k) === i)
  // What no step fixes (the Organization's Workspace limit): shown on every
  // step, it does not stop moving between them, only creating.
  const general = computed(() => errors.value[''] ?? '')
  // A step shows its errors once the person has been on it.
  const marked = (i: number) => visited[i] && i !== step.value && hasError(i)
  const blocked = computed(() => !!general.value || steps.some((_, i) => i !== review && hasError(i)))

  // check asks the server about the whole form; false when it refused.
  async function check(): Promise<boolean> {
    checking.value = true
    try {
      const r = await validate(toCreate(form))
      server.value = {}
      block.value = r.network
      return true
    } catch (err) {
      if (!(err instanceof ApiError) || err.status === 0) throw err
      const [field, words] = fromError(err)
      server.value = { [field]: words }
      return false
    } finally {
      checking.value = false
    }
  }

  // go moves to step i. Backward always; forward only when the step being
  // left has no error, the server's included: a refusal about a later step
  // is kept for when the person gets there.
  async function go(i: number): Promise<boolean> {
    if (i <= step.value) {
      step.value = i
      visited[i] = true
      return true
    }
    visited[step.value] = true
    if (hasError(step.value)) return false
    await check()
    // Every step up to i must pass; a refusal about an earlier one stops
    // there, a later one does not.
    for (let s = step.value; s < i; s++) {
      if (hasError(s)) {
        step.value = s
        visited[s] = true
        return false
      }
      visited[s] = true
    }
    step.value = i
    visited[i] = true
    return true
  }

  return {
    form,
    step,
    visited,
    errors,
    general,
    hasError,
    marked,
    blocked,
    checking,
    block,
    check,
    go,
    next: () => go(step.value + 1),
    back: () => go(Math.max(0, step.value - 1)),
    // place puts a refusal of the create itself where it belongs and opens
    // its step.
    place(err: ApiError) {
      const [field, words] = fromError(err)
      server.value = { [field]: words }
      const s = stepOf(field)
      if (s >= 0) {
        step.value = s
        visited[s] = true
      }
    },
  }
}
