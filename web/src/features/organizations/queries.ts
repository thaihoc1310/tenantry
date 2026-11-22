// Every call the organization screens make (IMPLEMENTATION-PLAN §6.2).
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { api, csrfToken, idempotencyKey, unwrap, type Schemas } from '@/api/client'

export type Organization = Schemas['Organization']

export function useMe() {
  return useQuery({ queryKey: ['me'], queryFn: () => unwrap(api.GET('/me')), staleTime: 60_000 })
}

// The caller's organizations, newest first, including their own Pending and
// Rejected requests. One page of 200 covers anyone's list in practice.
export function useOrganizations() {
  return useQuery({
    queryKey: ['organizations'],
    queryFn: async () => (await unwrap(api.GET('/organizations', { params: { query: { limit: 200 } } }))).items,
  })
}

export function useOrganization(idOrSlug: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => ['organization', toValue(idOrSlug)]),
    queryFn: () => unwrap(api.GET('/organizations/{orgId}', { params: { path: { orgId: toValue(idOrSlug) } } })),
  })
}

export function useUsage(idOrSlug: MaybeRefOrGetter<string>, enabled: MaybeRefOrGetter<boolean>) {
  return useQuery({
    queryKey: computed(() => ['usage', toValue(idOrSlug)]),
    queryFn: () => unwrap(api.GET('/organizations/{orgId}/usage', { params: { path: { orgId: toValue(idOrSlug) } } })),
    enabled: computed(() => toValue(enabled)),
  })
}

// useCreateOrganization sends a request for a new organization; one key per
// form, so a double click or a retry files it once.
export function useCreateOrganization() {
  const qc = useQueryClient()
  let key = idempotencyKey()
  return useMutation({
    mutationFn: (body: Schemas['OrganizationCreate']) =>
      unwrap(api.POST('/organizations', { body, params: { header: { 'Idempotency-Key': key } } })),
    onSuccess: () => {
      key = idempotencyKey() // the next request is another change
      return qc.invalidateQueries({ queryKey: ['organizations'] })
    },
  })
}

// logout ends the portal session and the Keycloak SSO session (Headlamp and
// kubectl too). It always ends with the cookies gone, so its answer only matters as a network failure.
export async function logout(): Promise<void> {
  await fetch('/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrfToken() } })
}
