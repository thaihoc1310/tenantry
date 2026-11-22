// The calls of the people screens: members, invitations, groups and the
// ownership transfer (IMPLEMENTATION-PLAN §6.2, M4).
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { ApiError, api, idempotencyKey, unwrap, type Schemas } from '@/api/client'

export type Member = Schemas['Member']
export type Invitation = Schemas['Invitation']
export type Group = Schemas['Group']
export type Transfer = Schemas['OwnershipTransfer']
export type InvitationRole = Schemas['InvitationRole']

// allPages follows the cursor to the end. Members and invitations are bounded
// by maxMembers, so the whole list is small; a screen shows it at once.
async function allPages<T>(page: (cursor?: string) => Promise<{ items: T[]; nextCursor?: string }>): Promise<T[]> {
  const out: T[] = []
  let cursor: string | undefined
  do {
    const p = await page(cursor)
    out.push(...p.items)
    cursor = p.nextCursor || undefined
  } while (cursor)
  return out
}

// Every screen of an organization reads its data under keys that start
// with ['org', idOrSlug], so one invalidation refreshes them all.
const orgKey = (org: string, ...rest: string[]) => ['org', org, ...rest]

export function useMembers(org: MaybeRefOrGetter<string>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: computed(() => orgKey(toValue(org), 'members')),
    enabled: computed(() => toValue(enabled)),
    queryFn: () =>
      allPages((cursor) =>
        unwrap(
          api.GET('/organizations/{orgId}/members', {
            params: { path: { orgId: toValue(org) }, query: { limit: 200, cursor } },
          }),
        ),
      ),
  })
}

export function useInvitations(org: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => orgKey(toValue(org), 'invitations')),
    queryFn: () =>
      allPages((cursor) =>
        unwrap(
          api.GET('/organizations/{orgId}/invitations', {
            params: { path: { orgId: toValue(org) }, query: { limit: 200, cursor } },
          }),
        ),
      ),
  })
}

// Groups refresh every few seconds while one waits for Keycloak, so the
// sync status changes on screen by itself.
export function useGroups(org: MaybeRefOrGetter<string>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: computed(() => orgKey(toValue(org), 'groups')),
    enabled: computed(() => toValue(enabled)),
    queryFn: async () =>
      (await unwrap(api.GET('/organizations/{orgId}/groups', { params: { path: { orgId: toValue(org) } } }))).items,
    refetchInterval: (q) =>
      q.state.data?.some((g) => g.syncStatus !== 'Synced' || g.members.some((m) => m.syncStatus !== 'Synced'))
        ? 3000
        : false,
  })
}

// useTransfer is the ownership transfer waiting for an answer, or null.
export function useTransfer(org: MaybeRefOrGetter<string>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: computed(() => orgKey(toValue(org), 'transfer')),
    enabled: computed(() => toValue(enabled)),
    queryFn: async () => {
      try {
        return await unwrap(
          api.GET('/organizations/{orgId}/ownership-transfer', { params: { path: { orgId: toValue(org) } } }),
        )
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
  })
}

// orgMutation runs a change of one organization and then refreshes
// everything shown about it, the organization list too (a role or the
// membership itself may have changed).
function orgMutation<V, R>(org: MaybeRefOrGetter<string>, call: (v: V) => Promise<R>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: call,
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ['org', toValue(org)] }),
        qc.invalidateQueries({ queryKey: ['organization'] }),
        qc.invalidateQueries({ queryKey: ['organizations'] }),
        qc.invalidateQueries({ queryKey: ['usage'] }),
      ]),
  })
}

// useInvite creates an invitation; one key per dialog opening (reset()).
export function useInvite(org: MaybeRefOrGetter<string>) {
  let key = idempotencyKey()
  const m = orgMutation(org, (body: Schemas['InvitationCreate']) =>
    unwrap(
      api.POST('/organizations/{orgId}/invitations', {
        params: { path: { orgId: toValue(org) }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  )
  return { ...m, newKey: () => (key = idempotencyKey()) }
}

export function useRevokeInvitation(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (id: string) =>
    unwrap(api.DELETE('/invitations/{invitation}', { params: { path: { invitation: id } } })),
  )
}

export function useSetRole(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (v: { userId: string; role: InvitationRole }) =>
    unwrap(
      api.PATCH('/organizations/{orgId}/members/{userId}', {
        params: { path: { orgId: toValue(org), userId: v.userId } },
        body: { role: v.role },
      }),
    ),
  )
}

export function useRemoveMember(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (userId: string) =>
    unwrap(
      api.DELETE('/organizations/{orgId}/members/{userId}', { params: { path: { orgId: toValue(org), userId } } }),
    ),
  )
}

export function useLeave(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, () =>
    unwrap(api.POST('/organizations/{orgId}/members/me:leave', { params: { path: { orgId: toValue(org) } } })),
  )
}

export function useDeleteOrganization(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, () =>
    unwrap(api.DELETE('/organizations/{orgId}', { params: { path: { orgId: toValue(org) } } })),
  )
}

export function usePatchOrganization(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (body: Schemas['OrganizationPatch']) =>
    unwrap(api.PATCH('/organizations/{orgId}', { params: { path: { orgId: toValue(org) } }, body })),
  )
}

export function useProposeTransfer(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (toUserId: string) =>
    unwrap(
      api.POST('/organizations/{orgId}:transfer-ownership', {
        params: { path: { orgId: toValue(org) } },
        body: { toUserId },
      }),
    ),
  )
}

export function useAcceptTransfer(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (transferId: string) =>
    unwrap(api.POST('/ownership-transfers/{transferId}:accept', { params: { path: { transferId } } })),
  )
}

// useCancelTransfer is the owner withdrawing the offer, or its recipient declining it.
export function useCancelTransfer(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (transferId: string) =>
    unwrap(api.DELETE('/ownership-transfers/{transferId}', { params: { path: { transferId } } })),
  )
}

// useCreateGroup: one key per dialog opening (newKey()).
export function useCreateGroup(org: MaybeRefOrGetter<string>) {
  let key = idempotencyKey()
  const m = orgMutation(org, (name: string) =>
    unwrap(
      api.POST('/organizations/{orgId}/groups', {
        params: { path: { orgId: toValue(org) }, header: { 'Idempotency-Key': key } },
        body: { name },
      }),
    ),
  )
  return { ...m, newKey: () => (key = idempotencyKey()) }
}

export function useRenameGroup(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (v: { groupId: string; name: string }) =>
    unwrap(api.PATCH('/groups/{groupId}', { params: { path: { groupId: v.groupId } }, body: { name: v.name } })),
  )
}

export function useDeleteGroup(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (groupId: string) =>
    unwrap(api.DELETE('/groups/{groupId}', { params: { path: { groupId } } })),
  )
}

export function useGroupMember(org: MaybeRefOrGetter<string>) {
  return orgMutation(org, (v: { groupId: string; userId: string; add: boolean }) => {
    const opts = { params: { path: { groupId: v.groupId, userId: v.userId } } }
    return unwrap(
      v.add
        ? api.PUT('/groups/{groupId}/members/{userId}', opts)
        : api.DELETE('/groups/{groupId}/members/{userId}', opts),
    )
  })
}

// The invitation link's page (logged in, not a member yet).
export function useInvitationPreview(token: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['invitation', toValue(token)]),
    queryFn: () => unwrap(api.GET('/invitations/{invitation}', { params: { path: { invitation: toValue(token) } } })),
    retry: false,
  })
}

export function useAcceptInvitation() {
  const qc = useQueryClient()
  let key = idempotencyKey()
  return useMutation({
    mutationFn: (token: string) =>
      unwrap(
        api.POST('/invitations/{token}:accept', {
          params: { path: { token }, header: { 'Idempotency-Key': key } },
        }),
      ),
    onSuccess: () => {
      key = idempotencyKey()
      return qc.invalidateQueries({ queryKey: ['organizations'] })
    },
  })
}
