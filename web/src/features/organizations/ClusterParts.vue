<script setup lang="ts">
// The pools and nodes of one granted cluster (docs/design/mockups: Clusters):
// shared pools as a summary, dedicated nodes with what is left on them, and
// the Pod network pools.
import { cores, gib } from '@/api/units'
import EmptyState from '@/components/EmptyState.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useOrgDedicatedNodes, useOrgNetworkPools, useOrgSharedPools } from './queries'

const props = defineProps<{ orgSlug: string; clusterId: string }>()
const pools = useOrgSharedPools(
  () => props.orgSlug,
  () => props.clusterId,
)
const nodes = useOrgDedicatedNodes(
  () => props.orgSlug,
  () => props.clusterId,
)
const nets = useOrgNetworkPools(
  () => props.orgSlug,
  () => props.clusterId,
)
</script>

<template>
  <TPanel
    v-for="p in pools.data.value ?? []"
    :key="p.id"
    title="Shared pool"
    note="nodes shared with other Organizations"
  >
    <div class="t-row t-between" style="margin-bottom: 16px">
      <code>{{ p.id }}</code
      ><span class="muted small">{{ p.nodeCount }} node{{ p.nodeCount === 1 ? '' : 's' }}</span>
    </div>
    <div class="t-meters two">
      <QuotaMeter
        label="CPU reserved, all tenants"
        :used="cores(p.capacity.allocated.cpu)"
        :total="cores(p.capacity.ceiling.cpu)"
        unit="cores"
      />
      <QuotaMeter
        label="Memory reserved, all tenants"
        :used="gib(p.capacity.allocated.memory)"
        :total="gib(p.capacity.ceiling.memory)"
        unit="Gi"
      />
    </div>
  </TPanel>

  <TPanel title="Dedicated nodes" note="only your Organization runs Pods here" flush>
    <div v-if="nodes.data.value?.length" class="t-tablewrap">
      <el-table scrollbar-always-on :data="nodes.data.value" style="width: 100%">
        <el-table-column label="Node" width="160"
          ><template #default="{ row }"
            ><code>{{ row.name }}</code></template
          ></el-table-column
        >
        <el-table-column label="Health" width="130"
          ><template #default="{ row }"><StatusTag :value="row.nodeState" /></template
        ></el-table-column>
        <el-table-column label="Capacity" width="180"
          ><template #default="{ row }"><StatusTag :value="row.capacityState" /></template
        ></el-table-column>
        <el-table-column label="CPU left" width="140" align="right"
          ><template #default="{ row }"
            ><span class="num">{{ cores(row.remaining.cpu) }} of {{ cores(row.reservable.cpu) }}</span></template
          ></el-table-column
        >
        <el-table-column label="Memory left" width="160" align="right"
          ><template #default="{ row }"
            ><span class="num">{{ gib(row.remaining.memory) }} of {{ gib(row.reservable.memory) }} Gi</span></template
          ></el-table-column
        >
        <el-table-column label="Used by" min-width="140"
          ><template #default="{ row }"
            ><span v-if="row.workspaces.length">{{ row.workspaces.join(', ') }}</span
            ><span v-else class="muted">No Workspace</span></template
          ></el-table-column
        >
      </el-table>
    </div>
    <EmptyState
      v-else
      title="No dedicated node"
      description="Nodes a Platform Admin gives this Organization appear here, with the capacity left on each."
    />
  </TPanel>

  <TPanel v-for="n in nets.data.value ?? []" :key="n.id" title="Pod network pool">
    <dl class="t-kv">
      <dt>Pool</dt>
      <dd>
        <code>{{ n.id }}</code> <span class="muted small">{{ n.shared ? 'shared' : 'your own' }}</span>
      </dd>
      <dt>Address range</dt>
      <dd class="mono">{{ n.cidrs.join(', ') }}</dd>
      <dt>Each Workspace gets</dt>
      <dd>a /{{ n.defaultPrefixLength }} block ({{ 2 ** (32 - n.defaultPrefixLength) - 2 }} Pod addresses)</dd>
    </dl>
  </TPanel>
</template>
