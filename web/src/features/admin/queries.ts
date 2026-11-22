import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { api, idempotencyKey, unwrap, type Schemas } from '@/api/client'

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
