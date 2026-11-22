// Routes mirror IMPLEMENTATION-PLAN §6.4; M4 has the ones below.
import { createRouter, createWebHistory } from 'vue-router'
import { api, unwrap } from '@/api/client'
import { queryClient } from '@/app/queryClient'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: () => import('@/features/onboarding/RequestPage.vue') },
    { path: '/request', component: () => import('@/features/onboarding/RequestPage.vue'), props: { another: true } },
    { path: '/login', component: () => import('@/features/onboarding/LoggedOutPage.vue'), meta: { public: true } },
    { path: '/orgs/:orgSlug', component: () => import('@/features/organizations/OverviewPage.vue'), props: true },
    { path: '/orgs/:orgSlug/members', component: () => import('@/features/people/MembersPage.vue'), props: true },
    { path: '/orgs/:orgSlug/groups', component: () => import('@/features/people/GroupsPage.vue'), props: true },
    { path: '/orgs/:orgSlug/settings', component: () => import('@/features/people/SettingsPage.vue'), props: true },
    { path: '/invitations/:token', component: () => import('@/features/people/InvitationPage.vue'), props: true },
    {
      path: '/admin',
      meta: { admin: true },
      children: [
        { path: '', redirect: '/admin/organizations' },
        { path: 'organizations', component: () => import('@/features/admin/AdminOrganizationsPage.vue') },
        {
          path: 'organizations/:orgId/review',
          component: () => import('@/features/admin/AdminReviewPage.vue'),
          props: true,
        },
      ],
    },
    { path: '/:rest(.*)*', component: () => import('@/app/NotFoundPage.vue'), meta: { public: true } },
  ],
})

// The admin area renders for Platform Admins only; the API checks again
// on every call (PRODUCT-PLAN §10.1).
router.beforeEach(async (to) => {
  if (!to.matched.some((r) => r.meta.admin)) return true
  const me = await queryClient.fetchQuery({
    queryKey: ['me'],
    queryFn: () => unwrap(api.GET('/me')),
    staleTime: 60_000,
  })
  return me?.isPlatformAdmin ? true : '/'
})
