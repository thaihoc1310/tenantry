import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { ApiError, type Schemas } from '@/api/client'
import { emptyForm, fill, fromError, localErrors, slugOf, stepOf, toCreate, useWorkspaceWizard } from './wizard'

const ok: Schemas['WorkspaceValidation'] = { valid: true, network: { networkPoolId: 'pods', cidr: '10.240.3.0/24' } }

const cluster = (id: string, isDefault: boolean, status: Schemas['OrgCluster']['status']): Schemas['OrgCluster'] => ({
  id,
  displayName: id,
  status,
  proxyEndpoint: '',
  consoleUrl: '',
  appsDomain: '',
  grantId: 'g-' + id,
  isDefault,
  sharedPools: 1,
  dedicatedNodes: 0,
})
const group = (id: string, name: string, syncStatus: Schemas['SyncStatus']): Schemas['Group'] => ({
  id,
  orgId: 'o',
  name,
  machineName: '',
  syncStatus,
  members: [],
  createdAt: '',
})
const cap = (cpu: string, memory: string, pods: number) => ({ cpu, memory, pods })

// A wizard filled as the seed's organization would be.
function wizard(validate = vi.fn(async () => ok)) {
  const w = useWorkspaceWizard(() => 'acme', validate)
  fill(w.form, {
    clusters: [cluster('cluster-a', true, 'Ready')],
    sharedPools: [
      {
        id: 'shared-general',
        name: 'General',
        nodeCount: 2,
        capacity: { ceiling: cap('8', '16Gi', 200), allocated: cap('0', '0', 0) },
      },
    ],
    networkPools: [
      { id: 'pods', cidrs: ['10.240.0.0/18'], defaultPrefixLength: 24, allowedPrefixLengths: [24, 23], shared: true },
    ],
    groups: [group('g1', 'admins', 'Synced'), group('g2', 'developers', 'Synced'), group('g3', 'viewers', 'Pending')],
  })
  return { w, validate }
}

describe('slug and fields', () => {
  it('derives a slug the API takes from the name', () => {
    expect(slugOf('Solar Prod!')).toBe('solar-prod')
    expect(slugOf('42 Orbit')).toBe('orbit')
    expect(slugOf('A very long workspace name here')).toBe('a-very-long-workspac')
    expect(slugOf('abcdefghijklmnopqrs tuv')).toBe('abcdefghijklmnopqrs')
  })
  it('puts each field on the step that edits it', () => {
    expect(stepOf('slug')).toBe(0)
    expect(stepOf('compute.dedicatedNodeGrantId')).toBe(1)
    expect(stepOf('resources.requestsCpu')).toBe(2)
    expect(stepOf('network.requestedCidr')).toBe(3)
    expect(stepOf('resources.namespaceQuota')).toBe(4)
    expect(stepOf('namespaces.0')).toBe(4)
    expect(stepOf('bindings.1.groupId')).toBe(5)
    expect(stepOf('')).toBe(-1)
  })
})

describe('fill', () => {
  it('chooses the default Ready cluster, the first pools and the admins and developers groups', () => {
    const { w } = wizard()
    expect(w.form).toMatchObject({
      clusterId: 'cluster-a',
      sharedPoolId: 'shared-general',
      networkPoolId: 'pods',
      owners: ['g1'],
      devs: ['g2'],
      viewers: [],
    })
  })
  it('skips a cluster in Maintenance, a node that is full or not Ready, and keeps choices made', () => {
    const f = emptyForm()
    f.owners = ['g3']
    const node = (
      id: string,
      nodeState: Schemas['OrgDedicatedNode']['nodeState'],
      capacityState: Schemas['OrgDedicatedNode']['capacityState'],
    ) => ({
      id,
      name: id,
      nodeState,
      capacityState,
      reservable: cap('4', '8Gi', 50),
      reserved: cap('0', '0', 0),
      remaining: cap('4', '8Gi', 50),
      workspaces: [],
    })
    fill(f, {
      clusters: [cluster('a', true, 'Maintenance'), cluster('b', false, 'Ready')],
      computeOptions: [
        { mode: 'Shared', available: false, reason: 'no pool' },
        { mode: 'Dedicated', available: true },
      ],
      nodes: [
        node('n1', 'NotReady', 'Available'),
        node('n2', 'Ready', 'FullyAllocated'),
        node('n3', 'Ready', 'PartiallyAllocated'),
      ],
      groups: [group('g1', 'admins', 'Synced')],
    })
    expect(f).toMatchObject({ clusterId: 'b', mode: 'Dedicated', nodeGrantId: 'n3', owners: ['g3'], devs: [] })
  })
})

describe('request body', () => {
  it('sends limits for Shared and the network pool only when Auto', async () => {
    const { w } = wizard()
    Object.assign(w.form, { name: 'Solar', rcpu: 0.5, rmem: 1.5, lcpu: 2, lmem: 4, storage: 5, namespaces: ['web'] })
    await nextTick()
    expect(toCreate(w.form)).toEqual({
      slug: 'solar',
      displayName: 'Solar',
      clusterId: 'cluster-a',
      compute: { mode: 'Shared', sharedPoolId: 'shared-general' },
      resources: {
        requestsCpu: '500m',
        requestsMemory: '1536Mi',
        limitsCpu: '2',
        limitsMemory: '4Gi',
        pods: 10,
        requestsStorage: '5Gi',
        namespaceQuota: 3,
      },
      namespaces: ['web'],
      network: { networkPoolId: 'pods' },
      bindings: [
        { groupId: 'g1', role: 'TenantOwner' },
        { groupId: 'g2', role: 'Developer' },
      ],
    })
  })
  it('leaves limits out for Dedicated, keeps them equal on screen, and sends the advanced block', async () => {
    const { w } = wizard()
    Object.assign(w.form, {
      mode: 'Dedicated',
      nodeGrantId: 'ng1',
      rcpu: 3,
      rmem: 6,
      netAuto: false,
      prefix: 23,
      cidr: ' 10.240.8.0/23 ',
    })
    await nextTick()
    expect([w.form.lcpu, w.form.lmem]).toEqual([3, 6])
    const body = toCreate(w.form)
    expect(body.compute).toEqual({ mode: 'Dedicated', dedicatedNodeGrantId: 'ng1' })
    expect(body.resources).not.toHaveProperty('limitsCpu')
    expect(body.network).toEqual({ networkPoolId: 'pods', prefixLength: 23, requestedCidr: '10.240.8.0/23' })
  })
})

describe('local checks', () => {
  it('names what is wrong under the field', () => {
    const f = emptyForm()
    Object.assign(f, { slug: '9x', lcpu: 0.5, nsQuota: 1, namespaces: ['web', 'API'], owners: ['g1'], devs: ['g1'] })
    const e = localErrors(f, 'acme')
    expect(Object.keys(e).sort()).toEqual(
      [
        'bindings',
        'clusterId',
        'displayName',
        'namespaces',
        'resources.limitsCpu',
        'resources.namespaceQuota',
        'slug',
      ].sort(),
    )
  })
  it('refuses a namespace whose full name is longer than 63', () => {
    const f = { ...emptyForm(), slug: 'a'.repeat(20), namespaces: ['b'.repeat(40)] }
    expect(localErrors(f, 'acme').namespaces).toContain('63')
  })
})

describe('server refusals', () => {
  it('say what is left, on the field they are about', () => {
    const quota = new ApiError(409, 'CLUSTER_GRANT_QUOTA_EXCEEDED', 'x', '', '', {
      resource: 'requestsCpu',
      limit: '2',
      allocated: '1500m',
      requested: '1',
    })
    expect(fromError(quota)).toEqual([
      'resources.requestsCpu',
      "Only 0.5 cores left in the Organization's grant. Lower this, or ask a Platform Admin for more. (CLUSTER_GRANT_QUOTA_EXCEEDED)",
    ])
    const node = new ApiError(409, 'DEDICATED_NODE_INSUFFICIENT_CAPACITY', 'x', '', '', {
      node: 'wk3',
      resource: 'requestsMemory',
      reservable: '14Gi',
      reserved: '8Gi',
    })
    expect(fromError(node)).toEqual([
      'resources.requestsMemory',
      'Only 6 Gi left on node wk3. (DEDICATED_NODE_INSUFFICIENT_CAPACITY)',
    ])
    expect(
      fromError(
        new ApiError(409, 'CLUSTER_GRANT_QUOTA_EXCEEDED', 'x', '', '', {
          resource: 'namespaces',
          limit: '6',
          allocated: '5',
        }),
      )[0],
    ).toBe('resources.namespaceQuota')
    expect(fromError(new ApiError(409, 'ENTITLEMENT_EXCEEDED', 'x'))[0]).toBe('')
    expect(fromError(new ApiError(409, 'SLUG_TAKEN', 'x', '', 'slug'))[0]).toBe('slug')
    expect(fromError(new ApiError(409, 'NETWORK_POOL_EXHAUSTED', 'x'))[0]).toBe('network.prefixLength')
  })
})

describe('moving between steps', () => {
  it('validates on leaving a step and stops on a refusal about it', async () => {
    const validate = vi.fn(async () => ok)
    const { w } = wizard(validate)
    expect(await w.next()).toBe(false) // no name yet: refused before asking the server
    expect(validate).not.toHaveBeenCalled()
    w.form.name = 'Solar'
    await nextTick()
    expect(w.form.slug).toBe('solar')
    validate.mockRejectedValueOnce(new ApiError(409, 'SLUG_TAKEN', 'taken', '', 'slug'))
    expect(await w.next()).toBe(false)
    expect(w.step.value).toBe(0)
    expect(w.errors.value.slug).toContain('SLUG_TAKEN')
    // Editing the slug by hand clears the stale refusal and stops following the name.
    w.form.slugEdited = true
    w.form.slug = 'solar-2'
    await nextTick()
    expect(w.errors.value.slug).toBeUndefined()
    expect(await w.next()).toBe(true)
    expect(w.step.value).toBe(1)
    expect(w.block.value?.cidr).toBe('10.240.3.0/24')
    expect(validate).toHaveBeenCalledTimes(2)
  })

  it('keeps a refusal about a later step for later, and marks a visited step that went wrong', async () => {
    const validate = vi.fn(async () => ok)
    const { w } = wizard(validate)
    w.form.name = 'Solar'
    await nextTick()
    validate.mockRejectedValueOnce(new ApiError(409, 'GROUP_SYNC_PENDING', 'x', '', '', { groupId: 'g1' }))
    expect(await w.next()).toBe(true) // the refusal is about Access
    expect(w.errors.value.bindings).toContain('GROUP_SYNC_PENDING')
    expect(w.marked(5)).toBe(false) // not visited yet
    expect(w.blocked.value).toBe(true)
    // Jumping ahead stops at the first step with an error.
    validate.mockRejectedValueOnce(
      new ApiError(409, 'COMPUTE_POOL_CAPACITY_EXCEEDED', 'x', '', '', {
        resource: 'requestsCpu',
        limit: '8',
        allocated: '7500m',
      }),
    )
    expect(await w.go(6)).toBe(false)
    expect(w.step.value).toBe(2)
    expect(w.errors.value['resources.requestsCpu']).toBe(
      'Only 0.5 cores left in the shared pool. (COMPUTE_POOL_CAPACITY_EXCEEDED)',
    )
    // Back is never checked; the step left with an error is marked.
    expect(await w.back()).toBe(true)
    expect(w.marked(2)).toBe(true)
  })

  it('lets the person move on past what no step fixes, but not create', async () => {
    const validate = vi.fn(async (): Promise<Schemas['WorkspaceValidation']> => {
      throw new ApiError(409, 'ENTITLEMENT_EXCEEDED', 'x', '', '', { limit: 'maxWorkspacesTotal' })
    })
    const { w } = wizard(validate)
    w.form.name = 'Solar'
    await nextTick()
    expect(await w.go(6)).toBe(true)
    expect(w.general.value).toContain('ENTITLEMENT_EXCEEDED')
    expect(w.blocked.value).toBe(true)
  })

  it('opens the step of a refused create', () => {
    const { w } = wizard()
    w.step.value = 6
    w.place(new ApiError(409, 'CIDR_OVERLAP', 'x', '', 'network.requestedCidr'))
    expect(w.step.value).toBe(3)
    expect(w.errors.value['network.requestedCidr']).toContain('CIDR_OVERLAP')
  })

  it('does not hide a failure to reach the server as a field error', async () => {
    const { w } = wizard(vi.fn(async () => Promise.reject(new ApiError(0, 'NETWORK', 'offline'))))
    w.form.name = 'Solar'
    await nextTick()
    await expect(w.next()).rejects.toThrow('offline')
    expect(w.checking.value).toBe(false)
  })
})
