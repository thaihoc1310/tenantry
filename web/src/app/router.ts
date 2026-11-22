// Routes mirror IMPLEMENTATION-PLAN §6.4; M5 has the ones below.
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
    {
      path: '/orgs/:orgSlug/workspaces',
      component: () => import('@/features/workspaces/WorkspacesPage.vue'),
      props: true,
    },
    {
      path: '/orgs/:orgSlug/workspaces/new',
      component: () => import('@/features/workspaces/WizardPage.vue'),
      props: true,
    },
    {
      path: '/orgs/:orgSlug/workspaces/:wsSlug',
      component: () => import('@/features/workspaces/WorkspacePage.vue'),
      props: true,
    },
    { path: '/orgs/:orgSlug/members', component: () => import('@/features/people/MembersPage.vue'), props: true },
    { path: '/orgs/:orgSlug/groups', component: () => import('@/features/people/GroupsPage.vue'), props: true },
    {
      path: '/orgs/:orgSlug/clusters',
      component: () => import('@/features/organizations/ClustersPage.vue'),
      props: true,
    },
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
        {
          path: 'organizations/:orgId',
          component: () => import('@/features/admin/AdminOrganizationPage.vue'),
          props: true,
        },
        { path: 'clusters', component: () => import('@/features/admin/AdminClustersPage.vue') },
        { path: 'onboarding-policy', component: () => import('@/features/admin/AdminPolicyPage.vue') },
        { path: 'grants', component: () => import('@/features/admin/AdminGrantsPage.vue') },
        {
          path: 'grants/:orgId/:clusterId',
          component: () => import('@/features/admin/AdminGrantPage.vue'),
          props: true,
        },
        { path: 'operations', component: () => import('@/features/admin/AdminOperationsPage.vue') },
        { path: 'users', component: () => import('@/features/admin/AdminUsersPage.vue') },
        { path: 'health', component: () => import('@/features/admin/AdminHealthPage.vue') },
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
