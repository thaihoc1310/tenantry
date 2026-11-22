// Every call the Workspace screens make (IMPLEMENTATION-PLAN §6.2).
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, onScopeDispose, ref, type MaybeRefOrGetter, toValue, watch } from 'vue'
import { api, unwrap, type Schemas } from '@/api/client'

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
