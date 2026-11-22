<script setup lang="ts">
// Where a Workspace's Pods run, with the pool or node's capacity
// (docs/design/mockups: Workspace, Compute). It reads the Organization's
// catalog, so only its OrgOwner and OrgAdmins mount it.
import { computed } from 'vue'
import { cores, gib } from '@/api/units'
import KeyValues from '@/components/KeyValues.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import StatusTag from '@/components/StatusTag.vue'
import { useOrgDedicatedNodes, useOrgSharedPools } from '@/features/organizations/queries'
import type { Workspace } from './queries'

const props = defineProps<{ orgSlug: string; ws: Workspace }>()
const shared = computed(() => props.ws.compute.mode === 'Shared')
const pools = useOrgSharedPools(
  () => props.orgSlug,
  () => props.ws.clusterId,
)
const nodes = useOrgDedicatedNodes(
  () => props.orgSlug,
  () => props.ws.clusterId,
)
const pool = computed(() => pools.data.value?.find((p) => p.id === props.ws.compute.sharedPoolId))
const node = computed(() => nodes.data.value?.find((n) => n.id === props.ws.compute.dedicatedNodeGrantId))
</script>

<template>
  <div class="t-stack" style="gap: 20px">
    <KeyValues
      :items="[
        ['Mode', shared ? 'Shared nodes' : 'Dedicated node'],
        [shared ? 'Pool' : 'Node', shared ? (ws.compute.sharedPoolId ?? '') : (node?.name ?? '')],
        ['Reserved', `${cores(ws.resources.requestsCpu)} cores, ${gib(ws.resources.requestsMemory)} Gi`],
        ['Quality of service', shared ? 'Burstable: limits may exceed requests' : 'Guaranteed: limits equal requests'],
      ]"
    >
      <template #v1="{ value }"
        ><code>{{ value }}</code> <StatusTag v-if="node" :value="node.nodeState"
      /></template>
    </KeyValues>
    <div v-if="pool" class="t-meters two">
      <QuotaMeter
        :label="`CPU reserved on ${pool.id}, all tenants`"
        :used="cores(pool.capacity.allocated.cpu)"
        :total="cores(pool.capacity.ceiling.cpu)"
        unit="cores"
      />
      <QuotaMeter
        :label="`Memory reserved on ${pool.id}, all tenants`"
        :used="gib(pool.capacity.allocated.memory)"
        :total="gib(pool.capacity.ceiling.memory)"
        unit="Gi"
      />
    </div>
    <div v-if="node" class="t-meters two">
      <QuotaMeter
        :label="`CPU reserved on ${node.name}`"
        :used="cores(node.reserved.cpu)"
        :total="cores(node.reservable.cpu)"
        unit="cores"
      />
      <QuotaMeter
        :label="`Memory reserved on ${node.name}`"
        :used="gib(node.reserved.memory)"
        :total="gib(node.reservable.memory)"
        unit="Gi"
      />
    </div>
  </div>
</template>
