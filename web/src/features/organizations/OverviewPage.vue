<script setup lang="ts">
// An Organization's Overview (docs/design/mockups: Overview). OrgOwner and
// OrgAdmin see its quota and limits; an OrgMember sees the short version,
// without them (PRODUCT-PLAN §9.2).
import { computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { Schemas } from '@/api/client'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useOrganization, useUsage } from './queries'

const props = defineProps<{ orgSlug: string }>()
const router = useRouter()

const org = useOrganization(() => props.orgSlug)
const role = computed(() => org.data.value?.myRole)
const readsQuota = computed(
  () => org.data.value?.status === 'Active' && (role.value === 'OrgOwner' || role.value === 'OrgAdmin'),
)
const usage = useUsage(() => props.orgSlug, readsQuota)

// A request still under review, or rejected, lives on the start page.
watch(
  () => org.data.value?.status,
  (s) => {
    if (s && s !== 'Active') router.replace('/')
  },
)

const grant = computed(
  () => usage.data.value?.clusterGrants.find((g) => g.isDefault) ?? usage.data.value?.clusterGrants[0],
)

type Meter = { label: string; used: number; total: number; unit?: string }
function meters(g: Schemas['GrantUsage']): Meter[] {
  const c = (q: Schemas['AllocatedQuantity'], label: string): Meter => ({
    label,
    used: cores(q.allocated),
    total: cores(q.limit),
    unit: 'cores',
  })
  const m = (q: Schemas['AllocatedQuantity'], label: string): Meter => ({
    label,
    used: gib(q.allocated),
    total: gib(q.limit),
    unit: 'Gi',
  })
  const n = (q: Schemas['AllocatedCount'], label: string): Meter => ({ label, used: q.allocated, total: q.limit })
  return [
    n(g.workspaces, 'Workspaces'),
    n(g.namespaces, 'Namespaces'),
    c(g.requestsCpu, 'Shared CPU requests'),
    m(g.requestsMemory, 'Shared memory requests'),
    c(g.limitsCpu, 'Shared CPU limits'),
    m(g.limitsMemory, 'Shared memory limits'),
    n(g.pods, 'Shared Pods'),
    m(g.requestsStorage, 'Storage'),
  ]
}

const fresh = computed(() => usage.data.value?.organization.workspaces.used === 0)
const startingQuota = computed(() => {
  const g = grant.value
  if (!g) return ''
  return `${g.workspaces.limit} Workspaces, ${cores(g.requestsCpu.limit)} CPU cores and ${gib(g.requestsMemory.limit)} Gi of memory`
})
const noWorkspacesYet = 'Creating Workspaces arrives with the next release of the portal.'
</script>

<template>
  <AppShell active="overview" :org-slug="orgSlug">
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="!org.data.value" :rows="8" animated />

    <template v-else>
      <PageHeader :title="org.data.value.displayName">
        <template #tags>
          <code>{{ org.data.value.slug }}</code
          ><StatusTag :value="org.data.value.status" /><el-tag type="info" effect="plain" round
            >You are {{ role }}</el-tag
          >
        </template>
        <template #actions>
          <el-tooltip :content="noWorkspacesYet" placement="bottom-end">
            <span
              ><el-button type="primary" disabled
                ><el-icon><Plus /></el-icon>&nbsp;Create Workspace</el-button
              ></span
            >
          </el-tooltip>
        </template>
      </PageHeader>

      <template v-if="readsQuota">
        <ErrorState v-if="usage.isError.value" :error="usage.error.value" @retry="usage.refetch()" />
        <el-skeleton v-else-if="!usage.data.value" :rows="6" animated />
        <template v-else>
          <el-alert
            v-if="fresh && grant"
            type="success"
            show-icon
            title="Your Organization was approved"
            :description="`You start with a small shared quota on ${grant.clusterId}: ${startingQuota}. Create your first Workspace to use it.`"
          />

          <TPanel
            v-for="g in usage.data.value.clusterGrants"
            :key="g.grantId"
            :title="`Quota on ${g.clusterId}`"
            note="reserved by your Workspaces"
          >
            <template v-if="g.isDefault && usage.data.value.clusterGrants.length > 1" #extra
              ><el-tag type="info" effect="plain" round>Default cluster</el-tag></template
            >
            <div class="t-meters">
              <QuotaMeter v-for="m in meters(g)" :key="m.label" v-bind="m" />
            </div>
          </TPanel>

          <TPanel>
            <EmptyState
              title="No Workspaces yet"
              description="A Workspace is an isolated part of the cluster with its own quota, network and members. Creating one takes about a minute and gives you a kubeconfig and a web console link."
            >
              <template #actions>
                <el-tooltip :content="noWorkspacesYet">
                  <span><el-button type="primary" disabled>Create your first Workspace</el-button></span>
                </el-tooltip>
              </template>
            </EmptyState>
          </TPanel>

          <div class="t-cols">
            <TPanel title="Organization limits">
              <KeyValues
                :items="[
                  [
                    'Members',
                    `${usage.data.value.organization.members.used} of ${usage.data.value.organization.members.limit}`,
                  ],
                  [
                    'Groups',
                    `${usage.data.value.organization.groups.used} of ${usage.data.value.organization.groups.limit}`,
                  ],
                  [
                    'Workspaces, all clusters',
                    `${usage.data.value.organization.workspaces.used} of ${usage.data.value.organization.workspaces.limit}`,
                  ],
                ]"
              />
            </TPanel>
          </div>
        </template>
      </template>

      <TPanel v-else>
        <EmptyState
          title="No Workspaces you can reach yet"
          description="The Workspaces your groups have a role in appear here. Ask an OrgOwner or OrgAdmin of this Organization to add one of your groups to a Workspace."
        />
      </TPanel>
    </template>
  </AppShell>
</template>
