// Every call the Workspace screens make (IMPLEMENTATION-PLAN §6.2).
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, onScopeDispose, ref, type MaybeRefOrGetter, toValue, watch } from 'vue'
import { api, idempotencyKey, unwrap, type Schemas } from '@/api/client'

export type Workspace = Schemas['Workspace']
export type Operation = Schemas['Operation']

// A Workspace whose operation is under way is refreshed every 2 s.
const busy = (w?: Workspace) => !!w && ['Provisioning', 'Updating', 'Deleting'].includes(w.state)

export function useWorkspaces(org: MaybeRefOrGetter<string>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: computed(() => ['workspaces', toValue(org)]),
    queryFn: async () =>
      (await unwrap(api.GET('/organizations/{orgId}/workspaces', { params: { path: { orgId: toValue(org) } } }))).items,
    enabled: computed(() => toValue(enabled)),
    refetchInterval: (q) => (q.state.data?.some(busy) ? 2000 : false),
  })
}

// useWorkspaceBySlug finds a Workspace of the organization by its slug (the
// route's), then follows it by id.
export function useWorkspaceBySlug(org: MaybeRefOrGetter<string>, slug: MaybeRefOrGetter<string>) {
  const list = useWorkspaces(org)
  const id = computed(() => list.data.value?.find((w) => w.slug === toValue(slug))?.id)
  const one = useQuery({
    queryKey: computed(() => ['workspace', id.value]),
    queryFn: () => unwrap(api.GET('/workspaces/{workspaceId}', { params: { path: { workspaceId: id.value! } } })),
    enabled: computed(() => !!id.value),
    refetchInterval: (q) => (busy(q.state.data) ? 2000 : false),
  })
  const missing = computed(() => list.isSuccess.value && !id.value)
  return { list, one, id, missing }
}

export function useWorkspaceUsage(id: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: computed(() => ['workspace-usage', toValue(id)]),
    queryFn: () =>
      unwrap(api.GET('/workspaces/{workspaceId}/usage', { params: { path: { workspaceId: toValue(id)! } } })),
    enabled: computed(() => !!toValue(id)),
    refetchInterval: 15_000,
  })
}

export function useWorkspaceOperations(id: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: computed(() => ['workspace-operations', toValue(id)]),
    queryFn: async () =>
      (
        await unwrap(
          api.GET('/workspaces/{workspaceId}/operations', {
            params: { path: { workspaceId: toValue(id)! }, query: { limit: 10 } },
          }),
        )
      ).items,
    enabled: computed(() => !!toValue(id)),
    refetchInterval: 5000,
  })
}

const finished = (op?: Operation) => !!op && ['Succeeded', 'Failed', 'Blocked'].includes(op.state)

// useOperation follows one operation as server-sent events (decision 7,
// IMPLEMENTATION-PLAN §12); where the stream fails it polls every 2 s.
export function useOperation(id: MaybeRefOrGetter<string | undefined>) {
  const qc = useQueryClient()
  const streaming = ref(false)
  const q = useQuery({
    queryKey: computed(() => ['operation', toValue(id)]),
    queryFn: () => unwrap(api.GET('/operations/{operationId}', { params: { path: { operationId: toValue(id)! } } })),
    enabled: computed(() => !!toValue(id)),
    refetchInterval: (s) => (streaming.value || finished(s.state.data) ? false : 2000),
  })
  let source: EventSource | undefined
  const close = () => {
    source?.close()
    source = undefined
    streaming.value = false
  }
  watch(
    () => toValue(id),
    (opId) => {
      close()
      if (!opId || typeof EventSource === 'undefined') return
      source = new EventSource(`/api/v1/operations/${opId}/events`)
      streaming.value = true
      source.addEventListener('operation', (e) => {
        const op = JSON.parse((e as MessageEvent).data) as Operation
        qc.setQueryData(['operation', opId], op)
        if (finished(op)) {
          close()
          void qc.invalidateQueries({ queryKey: ['workspace'] })
          void qc.invalidateQueries({ queryKey: ['workspaces'] })
          void qc.invalidateQueries({ queryKey: ['workspace-operations'] })
        }
      })
      source.onerror = close // the poll takes over
    },
    { immediate: true },
  )
  onScopeDispose(close)
  return q
}

// Every Workspace change refreshes the Workspace screens.
function useWorkspaceChange<V, R>(call: (v: V) => Promise<R>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: call,
    onSettled: () =>
      Promise.all(
        ['workspace', 'workspaces', 'workspace-usage', 'workspace-operations', 'usage'].map((k) =>
          qc.invalidateQueries({ queryKey: [k] }),
        ),
      ),
  })
}

// The wizard's check of the whole form (POST .../workspaces:validate).
export function validateWorkspace(org: string, body: Schemas['WorkspaceCreate']) {
  return unwrap(api.POST('/organizations/{orgId}/workspaces:validate', { params: { path: { orgId: org } }, body }))
}

// useCreateWorkspace: one Idempotency-Key per wizard, so a double click or a
// retry after a lost answer creates one Workspace; a new key after success.
export function useCreateWorkspace(org: MaybeRefOrGetter<string>) {
  let key = idempotencyKey()
  return useWorkspaceChange(async (body: Schemas['WorkspaceCreate']) => {
    const r = await unwrap(
      api.POST('/organizations/{orgId}/workspaces', {
        params: { path: { orgId: toValue(org) }, header: { 'Idempotency-Key': key } },
        body,
      }),
    )
    key = idempotencyKey()
    return r
  })
}

export function usePatchWorkspace() {
  return useWorkspaceChange((v: { id: string; body: Schemas['WorkspacePatch'] }) =>
    unwrap(api.PATCH('/workspaces/{workspaceId}', { params: { path: { workspaceId: v.id } }, body: v.body })),
  )
}

export function useCordon() {
  return useWorkspaceChange((v: { id: string; cordon: boolean }) =>
    v.cordon
      ? unwrap(api.POST('/workspaces/{workspaceId}:cordon', { params: { path: { workspaceId: v.id } } }))
      : unwrap(api.POST('/workspaces/{workspaceId}:uncordon', { params: { path: { workspaceId: v.id } } })),
  )
}

export function useDeleteWorkspace() {
  return useWorkspaceChange((v: { id: string; confirm: string }) =>
    unwrap(
      api.DELETE('/workspaces/{workspaceId}', {
        params: { path: { workspaceId: v.id }, query: { confirmName: v.confirm } },
      }),
    ),
  )
}

export function useRetry() {
  return useWorkspaceChange((opId: string) =>
    unwrap(api.POST('/operations/{operationId}:retry', { params: { path: { operationId: opId } } })),
  )
}

// What is left in a network pool, and the Organization's own blocks
// (OrgOwner and OrgAdmins only).
export function useNetworkAvailability(org: MaybeRefOrGetter<string>, pool: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: computed(() => ['network-availability', toValue(org), toValue(pool)]),
    queryFn: () =>
      unwrap(
        api.GET('/organizations/{orgId}/network-pools/{poolId}/availability', {
          params: { path: { orgId: toValue(org), poolId: toValue(pool)! } },
        }),
      ),
    enabled: computed(() => !!toValue(pool)),
  })
}

// ---- Access roles and Namespaces (M10) ----------------------------------------

export function useWorkspaceBindings(id: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: computed(() => ['workspace-bindings', toValue(id)]),
    queryFn: async () =>
      (await unwrap(api.GET('/workspaces/{workspaceId}/bindings', { params: { path: { workspaceId: toValue(id)! } } })))
        .items,
    enabled: computed(() => !!toValue(id)),
  })
}

// Namespaces refresh every 2 s while one is being created or deleted.
export function useWorkspaceNamespaces(id: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: computed(() => ['workspace-namespaces', toValue(id)]),
    queryFn: async () =>
      (
        await unwrap(
          api.GET('/workspaces/{workspaceId}/namespaces', { params: { path: { workspaceId: toValue(id)! } } }),
        )
      ).items,
    enabled: computed(() => !!toValue(id)),
    refetchInterval: (q) => (q.state.data?.some((n) => n.state !== 'Active') ? 2000 : false),
  })
}

// Every binding or Namespace change also refreshes those lists.
function useAccessChange<V, R>(call: (v: V) => Promise<R>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: call,
    onSettled: () =>
      Promise.all(
        ['workspace', 'workspace-operations', 'workspace-usage', 'workspace-bindings', 'workspace-namespaces'].map(
          (k) => qc.invalidateQueries({ queryKey: [k] }),
        ),
      ),
  })
}

export function usePutBinding() {
  return useAccessChange((v: { id: string; groupId: string; role: Schemas['WorkspaceRole'] }) =>
    unwrap(
      api.PUT('/workspaces/{workspaceId}/bindings/{groupId}', {
        params: { path: { workspaceId: v.id, groupId: v.groupId } },
        body: { role: v.role },
      }),
    ),
  )
}

export function useDeleteBinding() {
  return useAccessChange((v: { id: string; groupId: string }) =>
    unwrap(
      api.DELETE('/workspaces/{workspaceId}/bindings/{groupId}', {
        params: { path: { workspaceId: v.id, groupId: v.groupId } },
      }),
    ),
  )
}

// A new Idempotency-Key after each Namespace created; a refusal is not kept.
export function useCreateNamespace() {
  let key = idempotencyKey()
  return useAccessChange(async (v: { id: string; name: string }) => {
    const r = await unwrap(
      api.POST('/workspaces/{workspaceId}/namespaces', {
        params: { path: { workspaceId: v.id }, header: { 'Idempotency-Key': key } },
        body: { name: v.name },
      }),
    )
    key = idempotencyKey()
    return r
  })
}

export function useDeleteNamespace() {
  return useAccessChange((v: { id: string; name: string }) =>
    unwrap(
      api.DELETE('/workspaces/{workspaceId}/namespaces/{name}', {
        params: { path: { workspaceId: v.id, name: v.name } },
      }),
    ),
  )
}

// ---- Routes (M11) ---------------------------------------------------------------

// Routes refresh every 2 s while one is on its way or going.
export function useWorkspaceRoutes(id: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: computed(() => ['workspace-routes', toValue(id)]),
    queryFn: async () =>
      (await unwrap(api.GET('/workspaces/{workspaceId}/routes', { params: { path: { workspaceId: toValue(id)! } } })))
        .items,
    enabled: computed(() => !!toValue(id)),
    refetchInterval: (q) => (q.state.data?.some((r) => r.state !== 'Active') ? 2000 : false),
  })
}

export function useCreateRoute() {
  const qc = useQueryClient()
  let key = idempotencyKey()
  return useMutation({
    mutationFn: async (v: { id: string; body: Schemas['WorkspaceRouteInput'] }) => {
      const r = await unwrap(
        api.POST('/workspaces/{workspaceId}/routes', {
          params: { path: { workspaceId: v.id }, header: { 'Idempotency-Key': key } },
          body: v.body,
        }),
      )
      key = idempotencyKey()
      return r
    },
    onSettled: () =>
      Promise.all(
        ['workspace', 'workspace-operations', 'workspace-routes'].map((k) => qc.invalidateQueries({ queryKey: [k] })),
      ),
  })
}

export function useDeleteRoute() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: string; routeId: string }) =>
      unwrap(
        api.DELETE('/workspaces/{workspaceId}/routes/{routeId}', {
          params: { path: { workspaceId: v.id, routeId: v.routeId } },
        }),
      ),
    onSettled: () =>
      Promise.all(
        ['workspace', 'workspace-operations', 'workspace-routes'].map((k) => qc.invalidateQueries({ queryKey: [k] })),
      ),
  })
}
