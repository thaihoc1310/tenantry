import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { ApiError, api, idempotencyKey, unwrap, type Schemas } from '@/api/client'
import type { paths } from '@/api/schema'

export type AdminOrganization = Schemas['AdminOrganization']
export type Approval = Schemas['Approval']
export type OrgStatus = Schemas['OrgStatus']

// Organizations by status, oldest first, a page at a time.
export function useAdminOrganizations(
  status: MaybeRefOrGetter<OrgStatus>,
  pageSize = 50,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  const q = useInfiniteQuery({
    enabled: computed(() => toValue(enabled)),
    queryKey: computed(() => ['admin-organizations', toValue(status), pageSize]),
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/admin/organizations', {
          params: { query: { status: toValue(status), limit: pageSize, cursor: pageParam || undefined } },
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
  })
  const items = computed(() => q.data.value?.pages.flatMap((p) => p.items) ?? [])
  return { ...q, items }
}

function useReview<T>(call: (key: string, vars: T) => Promise<unknown>) {
  const qc = useQueryClient()
  let key = idempotencyKey()
  return useMutation({
    mutationFn: (vars: T) => call(key, vars),
    onSuccess: () => {
      key = idempotencyKey()
      return qc.invalidateQueries({ queryKey: ['admin-organizations'] })
    },
  })
}

export function useApprove() {
  return useReview((key, v: { orgId: string; approval: Approval }) =>
    unwrap(
      api.POST('/admin/organizations/{orgId}:approve', {
        params: { path: { orgId: v.orgId }, header: { 'Idempotency-Key': key } },
        body: v.approval,
      }),
    ),
  )
}

export function useReject() {
  return useReview((key, v: { orgId: string; reason: string }) =>
    unwrap(
      api.POST('/admin/organizations/{orgId}:reject', {
        params: { path: { orgId: v.orgId }, header: { 'Idempotency-Key': key } },
        body: { reason: v.reason },
      }),
    ),
  )
}

// ---- M5: what a Platform Admin manages (PRODUCT-PLAN §16.5) -------------

export type AdminCluster = Schemas['AdminCluster']
export type SharedPool = Schemas['SharedPool']
export type NetworkPool = Schemas['NetworkPool']
export type ClusterNode = Schemas['ClusterNode']
export type DedicatedNode = Schemas['DedicatedNode']
export type ClusterGrant = Schemas['ClusterGrant']
export type AdminUser = Schemas['AdminUser']

// Every admin change refreshes everything under ['admin'] and the review queue.
function useAdminChange<V, R>(call: (v: V) => Promise<R>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: call,
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ['admin'] }),
        qc.invalidateQueries({ queryKey: ['admin-organizations'] }),
      ]),
  })
}

// keyed: a mutation whose idempotency key changes once it succeeded, and on newKey().
function useKeyedChange<V, R>(call: (key: string, v: V) => Promise<R>) {
  let key = idempotencyKey()
  const m = useAdminChange(async (v: V) => {
    const r = await call(key, v)
    key = idempotencyKey()
    return r
  })
  return { ...m, newKey: () => (key = idempotencyKey()) }
}

export function useAdminOrganization(orgId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['admin', 'organization', toValue(orgId)]),
    queryFn: () => unwrap(api.GET('/admin/organizations/{orgId}', { params: { path: { orgId: toValue(orgId) } } })),
  })
}

export function useCreateOrganization() {
  return useKeyedChange((key, body: Schemas['AdminOrganizationCreate']) =>
    unwrap(api.POST('/admin/organizations', { body, params: { header: { 'Idempotency-Key': key } } })),
  )
}

export function useSetEntitlement() {
  return useAdminChange((v: { orgId: string; entitlement: Schemas['Entitlement'] }) =>
    unwrap(
      api.PUT('/admin/organizations/{orgId}/entitlement', {
        params: { path: { orgId: v.orgId } },
        body: v.entitlement,
      }),
    ),
  )
}

export function useUsers(query: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['admin', 'users', toValue(query)]),
    queryFn: () =>
      unwrap(api.GET('/admin/users', { params: { query: { q: toValue(query) || undefined, limit: 50 } } })),
  })
}

export function useSetUserEnabled() {
  return useAdminChange((v: { userId: string; enabled: boolean }) =>
    unwrap(
      v.enabled
        ? api.POST('/admin/users/{userId}:enable', { params: { path: { userId: v.userId } } })
        : api.POST('/admin/users/{userId}:disable', { params: { path: { userId: v.userId } } }),
    ),
  )
}

export function useClusters() {
  return useQuery({
    queryKey: ['admin', 'clusters'],
    queryFn: async () => (await unwrap(api.GET('/admin/clusters'))).items,
  })
}

export function useRegisterCluster() {
  return useKeyedChange((key, body: Schemas['ClusterCreate']) =>
    unwrap(api.POST('/admin/clusters', { body, params: { header: { 'Idempotency-Key': key } } })),
  )
}

export function usePatchCluster() {
  return useAdminChange((v: { clusterId: string; patch: Schemas['ClusterPatch'] }) =>
    unwrap(api.PATCH('/admin/clusters/{clusterId}', { params: { path: { clusterId: v.clusterId } }, body: v.patch })),
  )
}

export function useClusterNodes(clusterId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['admin', 'nodes', toValue(clusterId)]),
    enabled: computed(() => !!toValue(clusterId)),
    queryFn: async () =>
      (
        await unwrap(
          api.GET('/admin/clusters/{clusterId}/nodes', { params: { path: { clusterId: toValue(clusterId) } } }),
        )
      ).items,
  })
}

export function useSharedPools(clusterId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['admin', 'shared-pools', toValue(clusterId)]),
    enabled: computed(() => !!toValue(clusterId)),
    queryFn: async () =>
      (
        await unwrap(
          api.GET('/admin/clusters/{clusterId}/shared-pools', { params: { path: { clusterId: toValue(clusterId) } } }),
        )
      ).items,
  })
}

export function useCreateSharedPool() {
  return useKeyedChange((key, v: { clusterId: string; body: Schemas['SharedPoolCreate'] }) =>
    unwrap(
      api.POST('/admin/clusters/{clusterId}/shared-pools', {
        params: { path: { clusterId: v.clusterId }, header: { 'Idempotency-Key': key } },
        body: v.body,
      }),
    ),
  )
}

export function usePutSharedPool() {
  return useAdminChange((v: { clusterId: string; poolId: string; body: Schemas['SharedPoolInput'] }) =>
    unwrap(
      api.PUT('/admin/clusters/{clusterId}/shared-pools/{poolId}', {
        params: { path: { clusterId: v.clusterId, poolId: v.poolId } },
        body: v.body,
      }),
    ),
  )
}

export function useNetworkPools(clusterId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['admin', 'network-pools', toValue(clusterId)]),
    enabled: computed(() => !!toValue(clusterId)),
    queryFn: async () =>
      (
        await unwrap(
          api.GET('/admin/clusters/{clusterId}/network-pools', { params: { path: { clusterId: toValue(clusterId) } } }),
        )
      ).items,
  })
}

export function useCreateNetworkPool() {
  return useKeyedChange((key, v: { clusterId: string; body: Schemas['NetworkPoolCreate'] }) =>
    unwrap(
      api.POST('/admin/clusters/{clusterId}/network-pools', {
        params: { path: { clusterId: v.clusterId }, header: { 'Idempotency-Key': key } },
        body: v.body,
      }),
    ),
  )
}

export function usePatchNetworkPool() {
  return useAdminChange((v: { clusterId: string; poolId: string; body: Schemas['NetworkPoolPatch'] }) =>
    unwrap(
      api.PATCH('/admin/clusters/{clusterId}/network-pools/{poolId}', {
        params: { path: { clusterId: v.clusterId, poolId: v.poolId } },
        body: v.body,
      }),
    ),
  )
}

export function usePutDedicatedNodes() {
  return useAdminChange((v: { orgId: string; clusterId: string; body: Schemas['DedicatedNodesInput'] }) =>
    unwrap(
      api.PUT('/admin/organizations/{orgId}/cluster-grants/{clusterId}/dedicated-nodes', {
        params: { path: { orgId: v.orgId, clusterId: v.clusterId } },
        body: v.body,
      }),
    ),
  )
}

export function usePolicy() {
  return useQuery({
    queryKey: ['admin', 'policy'],
    queryFn: async () => {
      try {
        return await unwrap(api.GET('/admin/onboarding-policy'))
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
  })
}

export function usePutPolicy() {
  return useAdminChange((body: Schemas['Approval']) => unwrap(api.PUT('/admin/onboarding-policy', { body })))
}

export function useAllGrants(clusterId: MaybeRefOrGetter<string> = '') {
  const q = useInfiniteQuery({
    queryKey: computed(() => ['admin', 'grants', toValue(clusterId)]),
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/admin/cluster-grants', {
          params: { query: { clusterId: toValue(clusterId) || undefined, limit: 100, cursor: pageParam || undefined } },
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
  })
  const items = computed(() => q.data.value?.pages.flatMap((p) => p.items) ?? [])
  return { ...q, items }
}

export function usePutGrant() {
  return useAdminChange((v: { orgId: string; clusterId: string; body: Schemas['ClusterGrantInput'] }) =>
    unwrap(
      api.PUT('/admin/organizations/{orgId}/cluster-grants/{clusterId}', {
        params: { path: { orgId: v.orgId, clusterId: v.clusterId } },
        body: v.body,
      }),
    ),
  )
}

export function useDeleteGrant() {
  return useAdminChange((v: { orgId: string; clusterId: string }) =>
    unwrap(
      api.DELETE('/admin/organizations/{orgId}/cluster-grants/{clusterId}', {
        params: { path: { orgId: v.orgId, clusterId: v.clusterId } },
      }),
    ),
  )
}

export type OperationState = NonNullable<
  NonNullable<NonNullable<paths['/admin/operations']['get']['parameters']['query']>['state']>
>

export function useAdminOperations(state: MaybeRefOrGetter<OperationState | ''>) {
  const q = useInfiniteQuery({
    queryKey: computed(() => ['admin', 'operations', toValue(state)]),
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/admin/operations', {
          params: { query: { state: toValue(state) || undefined, limit: 50, cursor: pageParam || undefined } },
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
    refetchInterval: 10_000,
  })
  const items = computed(() => q.data.value?.pages.flatMap((p) => p.items) ?? [])
  return { ...q, items }
}

export function useRetryOperation() {
  return useAdminChange((id: string) =>
    unwrap(api.POST('/operations/{operationId}:retry', { params: { path: { operationId: id } } })),
  )
}

// useChangeDedicatedNodes edits the set of nodes an organization holds on a
// cluster: the API takes the whole set, so the current one (with each node's
// system reserve) is read first.
export function useChangeDedicatedNodes() {
  const qc = useQueryClient()
  const put = usePutDedicatedNodes()
  return {
    ...put,
    change: async (
      orgId: string,
      clusterId: string,
      edit: (nodes: Schemas['DedicatedNodesInput']['nodes']) => Schemas['DedicatedNodesInput']['nodes'],
    ) => {
      const d = await qc.fetchQuery({
        queryKey: ['admin', 'organization', orgId],
        queryFn: () => unwrap(api.GET('/admin/organizations/{orgId}', { params: { path: { orgId } } })),
        staleTime: 0,
      })
      const current = (d.dedicatedNodes ?? [])
        .filter((n) => n.clusterId === clusterId)
        .map((n) => ({ name: n.name, systemReserve: n.systemReserve }))
      return put.mutateAsync({ orgId, clusterId, body: { nodes: edit(current) } })
    },
  }
}

// The Platform health page; refreshed like the operations list.
export function usePlatformHealth() {
  return useQuery({
    queryKey: ['admin', 'health'],
    queryFn: () => unwrap(api.GET('/admin/health')),
    refetchInterval: 10_000,
  })
}
