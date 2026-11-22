<script setup lang="ts">
// Top bar, menu, content (docs/design/mockups: AppShell). The menu lists the
// screens that exist; more join it milestone by milestone (Workspaces,
// Clusters and Audit with M5–M6).
import { useQueryClient } from '@tanstack/vue-query'
import { ElMessage } from 'element-plus'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAdminOrganizations } from '@/features/admin/queries'
import { logout, useMe, useOrganizations } from '@/features/organizations/queries'

const props = withDefaults(
  defineProps<{ area?: 'org' | 'admin' | 'bare'; active?: string; narrow?: boolean; orgSlug?: string }>(),
  { area: 'org', active: '', orgSlug: '' },
)

const router = useRouter()
const qc = useQueryClient()
const me = useMe()
const orgs = useOrganizations()

const initials = computed(() => {
  const words = (me.data.value?.displayName ?? '').split(/\s+/).filter(Boolean)
  return (words.length > 1 ? words[0]![0]! + words.at(-1)![0]! : (words[0] ?? '?').slice(0, 2)).toUpperCase()
})
const activeOrgs = computed(() => (orgs.data.value ?? []).filter((o) => o.status === 'Active'))
const current = computed(() => activeOrgs.value.find((o) => o.slug === props.orgSlug))
const isAdmin = computed(() => me.data.value?.isPlatformAdmin === true)

// The Organization menu (mockups: AppShell). An OrgMember gets the short one:
// Overview and Settings, where they can leave (PRODUCT-PLAN §16.2).
const orgNav = computed(() => {
  const all = [
    ['overview', 'Overview', '', 'Odometer'],
    ['members', 'Members', '/members', 'User'],
    ['groups', 'Groups', '/groups', 'Collection'],
    ['clusters', 'Clusters', '/clusters', 'Cpu'],
    ['settings', 'Settings', '/settings', 'Setting'],
  ]
  const role = current.value?.myRole
  return role === 'OrgOwner' || role === 'OrgAdmin' ? all : all.filter(([k]) => k === 'overview' || k === 'settings')
})

// The Platform Admin menu (mockups: AppShell); Platform health comes later.
const adminNav = [
  ['organizations', 'Organizations', '/admin/organizations', 'OfficeBuilding'],
  ['clusters', 'Clusters', '/admin/clusters', 'Cpu'],
  ['policy', 'Onboarding policy', '/admin/onboarding-policy', 'DocumentChecked'],
  ['grants', 'Grants', '/admin/grants', 'Coin'],
  ['operations', 'Operations', '/admin/operations', 'Operation'],
  ['users', 'Users', '/admin/users', 'UserFilled'],
]

// The review queue's length, for the admin menu.
const pending = useAdminOrganizations('Pending', 200, () => isAdmin.value && props.area === 'admin')
const waiting = computed(() => pending.items.value.length)

async function signOut() {
  try {
    await logout()
  } catch {
    ElMessage.error('The platform could not be reached; you are still logged in. Try again.')
    return
  }
  qc.clear()
  router.replace('/login')
}
</script>

<template>
  <div class="t-shell">
    <header class="t-top">
      <RouterLink
        class="t-brand"
        :to="area === 'admin' ? '/admin/organizations' : current ? `/orgs/${current.slug}` : '/'"
      >
        <span class="t-brand-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>Tenantry
      </RouterLink>
      <template v-if="area === 'org' && current">
        <span class="t-top-sep" aria-hidden="true"></span>
        <el-dropdown trigger="click">
          <button class="t-switch" type="button" :aria-label="'Switch Organization, current: ' + current.displayName">
            {{ current.displayName }} <el-icon><ArrowDown /></el-icon>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-for="o in activeOrgs" :key="o.id" @click="router.push(`/orgs/${o.slug}`)">
                <component :is="o.slug === current.slug ? 'strong' : 'span'">{{ o.displayName }}</component
                >&nbsp;<span class="muted">{{ o.myRole }}</span>
              </el-dropdown-item>
              <el-dropdown-item divided @click="router.push('/request')">Request another Organization</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>
      <el-tag v-if="area === 'admin'" type="primary" effect="plain" round>Platform Admin</el-tag>
      <span class="t-top-spacer"></span>
      <RouterLink v-if="area === 'admin'" to="/" class="small">Back to portal</RouterLink>
      <el-dropdown v-if="me.data.value" trigger="click">
        <button class="t-user" type="button" aria-label="Account menu">
          <span class="t-avatar" aria-hidden="true">{{ initials }}</span
          ><span class="t-user-name">{{ me.data.value.email ?? me.data.value.displayName }}</span
          ><el-icon><ArrowDown /></el-icon>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item disabled>{{ me.data.value.displayName }}</el-dropdown-item>
            <el-dropdown-item v-if="isAdmin && area !== 'admin'" @click="router.push('/admin/organizations')"
              >Platform Admin area</el-dropdown-item
            >
            <el-dropdown-item divided @click="signOut">Log out everywhere</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </header>

    <div v-if="area === 'bare'" class="t-bare">
      <div class="t-page" :class="{ narrow }"><slot></slot></div>
    </div>
    <div v-else class="t-body">
      <aside class="t-side">
        <div class="t-side-in">
          <div class="t-side-label">{{ area === 'admin' ? 'Platform' : 'Organization' }}</div>
          <nav class="t-nav" :aria-label="area === 'admin' ? 'Platform Admin' : 'Organization'">
            <template v-if="area === 'org'">
              <RouterLink
                v-for="[key, label, path, icon] in orgNav"
                :key="key"
                :to="`/orgs/${orgSlug}${path}`"
                :aria-current="active === key ? 'page' : undefined"
                ><el-icon><component :is="icon" /></el-icon>{{ label }}</RouterLink
              >
            </template>
            <template v-else>
              <RouterLink
                v-for="[key, label, path, icon] in adminNav"
                :key="key"
                :to="path!"
                :aria-current="active === key ? 'page' : undefined"
                ><el-icon><component :is="icon" /></el-icon>{{ label
                }}<span
                  v-if="key === 'organizations' && waiting"
                  class="t-nav-count"
                  :aria-label="waiting + ' waiting'"
                  >{{ waiting }}</span
                ></RouterLink
              >
            </template>
          </nav>
        </div>
      </aside>
      <main class="t-main">
        <div class="t-page" :class="{ narrow }"><slot></slot></div>
      </main>
    </div>
  </div>
</template>
