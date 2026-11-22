<script setup lang="ts">
// A Workspace's Pod network (docs/design/mockups: Workspace, Network): its
// address block, and for the OrgOwner and OrgAdmins what is left in the pool.
import { computed } from 'vue'
import KeyValues from '@/components/KeyValues.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import StatusTag from '@/components/StatusTag.vue'
import type { Workspace } from './queries'
import { useNetworkAvailability } from './queries'

const props = defineProps<{ orgSlug: string; ws: Workspace; manages: boolean }>()
const net = computed(() => props.ws.network)
const availability = useNetworkAvailability(
  () => props.orgSlug,
  () => (props.manages ? net.value?.networkPoolId : undefined),
)
// Pod addresses of a block: all but the network and broadcast addresses.
const addresses = computed(() => {
  const bits = Number(net.value?.cidr.split('/')[1] ?? 32)
  return Math.max(2 ** (32 - bits) - 2, 0)
})
</script>

<template>
  <p v-if="!net" class="muted">
    This Workspace was created before Pod networks were given out: its Pods use the cluster's shared range and are not
    isolated from other Workspaces.
  </p>
  <div v-else class="t-stack" style="gap: 20px">
    <KeyValues
      :items="[
        ['Address block', net.cidr],
        ['Pool', net.networkPoolId],
        ['Pod addresses', String(addresses)],
        ['Isolation', 'Pods of other Workspaces cannot connect to these Pods'],
      ]"
    >
      <template #v0
        ><span class="mono">{{ net.cidr }}</span> <StatusTag :value="net.state"
      /></template>
      <template #v1
        ><code>{{ net.networkPoolId }}</code></template
      >
    </KeyValues>
    <div v-if="availability.data.value" class="t-meters two">
      <QuotaMeter
        v-for="f in availability.data.value.free"
        :key="f.prefixLength"
        :label="`Free /${f.prefixLength} blocks in ${availability.data.value.id}`"
        :used="0"
        :total="f.blocks"
      />
    </div>
  </div>
</template>
