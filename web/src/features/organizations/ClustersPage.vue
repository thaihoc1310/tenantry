<script setup lang="ts">
// Granted Clusters (docs/design/mockups: Clusters; PRODUCT-PLAN §16.3): what
// a Platform Admin has granted this Organization on each cluster, its shared
// pools as a summary, the nodes dedicated to it with their remaining
// capacity, and its Pod network pools. OrgOwner and OrgAdmins only (§9.2).
import { computed } from 'vue'
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
import ClusterParts from './ClusterParts.vue'
import { useOrganization, useOrgClusters, useUsage } from './queries'

const props = defineProps<{ orgSlug: string }>()
const slug = () => props.orgSlug

const org = useOrganization(slug)
const manages = computed(
  () => org.data.value?.status === 'Active' && ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''),
)
const clusters = useOrgClusters(slug, manages)
const usage = useUsage(slug, manages)
const usageOf = (grantId: string) => usage.data.value?.clusterGrants.find((g) => g.grantId === grantId)

type Meter = { label: string; used: number; total: number; unit?: string }
function meters(g: Schemas['GrantUsage']): Meter[] {
  return [
    { label: 'Workspaces', used: g.workspaces.allocated, total: g.workspaces.limit },
    { label: 'Namespaces', used: g.namespaces.allocated, total: g.namespaces.limit },
    {
      label: 'Shared CPU requests',
      used: cores(g.requestsCpu.allocated),
      total: cores(g.requestsCpu.limit),
      unit: 'cores',
    },
    {
      label: 'Shared memory requests',
      used: gib(g.requestsMemory.allocated),
      total: gib(g.requestsMemory.limit),
      unit: 'Gi',
    },
    { label: 'Storage', used: gib(g.requestsStorage.allocated), total: gib(g.requestsStorage.limit), unit: 'Gi' },
  ]
}
</script>

<template>
  <AppShell active="clusters" :org-slug="orgSlug">
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="!org.data.value" :rows="8" animated />
    <TPanel v-else-if="!manages">
      <EmptyState
        title="Only the owner and admins see the clusters"
        description="What a Platform Admin has granted the Organization is shown to its OrgOwner and OrgAdmins."
      />
    </TPanel>

    <template v-else>
      <PageHeader
        title="Clusters"
        description="What a Platform Admin has granted this Organization. Only an Admin can change these numbers."
      />
      <ErrorState v-if="clusters.isError.value" :error="clusters.error.value" @retry="clusters.refetch()" />
      <el-skeleton v-else-if="clusters.isPending.value" :rows="6" animated />
      <TPanel v-else-if="!clusters.data.value?.length">
        <EmptyState title="No cluster granted" description="Ask a Platform Admin for a grant on a cluster." />
      </TPanel>

      <template v-for="c in clusters.data.value ?? []" :key="c.id">
        <TPanel :title="c.displayName" :note="c.isDefault ? 'default cluster' : c.id">
          <template #extra
            ><StatusTag :value="c.status" :label="c.status === 'Ready' ? 'Cluster ready' : `Cluster ${c.status}`"
          /></template>
          <div class="t-stack" style="gap: 24px">
            <div v-if="usageOf(c.grantId)" class="t-meters">
              <QuotaMeter v-for="m in meters(usageOf(c.grantId)!)" :key="m.label" v-bind="m" />
            </div>
            <KeyValues
              :items="[
                ['kubectl endpoint', c.proxyEndpoint],
                ['Web console', c.consoleUrl],
                ['App domain', '*.' + c.appsDomain],
              ]"
            >
              <template #v0
                ><span class="mono">{{ c.proxyEndpoint }}</span></template
              >
              <template #v1
                ><span class="mono">{{ c.consoleUrl }}</span></template
              >
              <template #v2
                ><span class="mono">*.{{ c.appsDomain }}</span></template
              >
            </KeyValues>
          </div>
        </TPanel>
        <ClusterParts :org-slug="orgSlug" :cluster-id="c.id" />
      </template>
    </template>
  </AppShell>
</template>
