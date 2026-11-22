<script setup lang="ts">
// Clusters registered with the platform, and the pools and nodes
// Organizations can be given on them (docs/design/mockups: AdminClusters;
// PRODUCT-PLAN §6, §7.1, §16.5). In M5 every change of pools and nodes is
// written to the database only; the operator follows from M6.
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { when } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import ClusterDialog from './ClusterDialog.vue'
import GiveNodeDialog from './GiveNodeDialog.vue'
import NetworkPoolDialog from './NetworkPoolDialog.vue'
import SharedPoolDialog from './SharedPoolDialog.vue'
import {
  type ClusterNode,
  type NetworkPool,
  type SharedPool,
  useChangeDedicatedNodes,
  useClusterNodes,
  useClusters,
  useNetworkPools,
  usePatchCluster,
  useSharedPools,
} from './queries'

const route = useRoute()
const router = useRouter()

const clusters = useClusters()
const selected = computed(() => {
  const all = clusters.data.value ?? []
  return all.find((c) => c.id === route.query.cluster) ?? all[0]
})
const clusterId = computed(() => selected.value?.id ?? '')
const nodes = useClusterNodes(clusterId)
const pools = useSharedPools(clusterId)
const nets = useNetworkPools(clusterId)
const patch = usePatchCluster()
const take = useChangeDedicatedNodes()

// The status changes allowed from each status (architecture/04 §6).
const next: Record<string, string[]> = {
  Registered: ['Ready'],
  Ready: ['Maintenance', 'Unavailable'],
  Maintenance: ['Ready', 'Unavailable'],
  Unavailable: ['Ready', 'Maintenance'],
}
const status = ref('')
watch(selected, (c) => (status.value = c?.status ?? ''), { immediate: true })

async function changeStatus(to: string) {
  const c = selected.value
  if (!c || to === c.status) return
  const effect: Record<string, string> = {
    Ready: 'Organizations can be granted the cluster and create Workspaces on it.',
    Maintenance: 'Existing Workspaces keep running and stay reachable; creating and resizing are paused for everyone.',
    Unavailable: 'Workspaces on it show as unknown and changes for it wait until it is back.',
  }
  try {
    await ElMessageBox.confirm(effect[to] ?? '', `Mark ${c.displayName} ${to}?`, { confirmButtonText: `Mark ${to}` })
  } catch {
    status.value = c.status
    return
  }
  try {
    await patch.mutateAsync({ clusterId: c.id, patch: { status: to as 'Ready' } })
    ElMessage.success(`${c.displayName} is ${to}.`)
  } catch (err) {
    status.value = c.status
    showFailure(err)
  }
}

const dedicated = computed(() => (nodes.data.value ?? []).filter((n) => n.dedicatedTo))

async function takeBack(n: ClusterNode) {
  const org = n.dedicatedTo!
  try {
    await ElMessageBox.confirm(
      `${org.displayName} can no longer place Workspaces on ${n.name}. The node becomes free; Pods on it keep running until you drain it.`,
      `Take ${n.name} back?`,
      { type: 'warning', confirmButtonText: 'Take back', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  try {
    await take.change(org.id, clusterId.value, (list) => list.filter((x) => x.name !== n.name))
    ElMessage.success(`${n.name} is free again.`)
  } catch (err) {
    showFailure(err)
  }
}

const registering = ref(false)
const editing = ref(false)
const poolOpen = ref(false)
const poolEdit = ref<SharedPool>()
const giving = ref(false)
const netOpen = ref(false)
const netEdit = ref<NetworkPool>()

function openPool(p?: SharedPool) {
  poolEdit.value = p
  poolOpen.value = true
}
function openNet(p?: NetworkPool) {
  netEdit.value = p
  netOpen.value = true
}
function pick(id: string) {
  router.replace({ query: { ...route.query, cluster: id } })
}
</script>

<template>
  <AppShell area="admin" active="clusters">
    <PageHeader
      title="Clusters"
      description="Clusters registered with the platform, and the pools and nodes Organizations can be given on them."
    >
      <template #actions
        ><el-button @click="registering = true"
          ><el-icon><Plus /></el-icon>&nbsp;Register cluster</el-button
        ></template
      >
    </PageHeader>

    <ErrorState v-if="clusters.isError.value" :error="clusters.error.value" @retry="clusters.refetch()" />
    <el-skeleton v-else-if="clusters.isPending.value" :rows="8" animated />
    <TPanel v-else-if="!selected">
      <EmptyState title="No cluster yet" description="Register the first cluster Workspaces will run on." />
    </TPanel>

    <template v-else>
      <nav v-if="(clusters.data.value ?? []).length > 1" class="t-tabs" aria-label="Clusters">
        <a
          v-for="c in clusters.data.value"
          :key="c.id"
          href="#"
          :aria-current="c.id === selected.id ? 'page' : undefined"
          @click.prevent="pick(c.id)"
          >{{ c.displayName }}</a
        >
      </nav>

      <TPanel :title="selected.displayName" :note="selected.id">
        <template #extra>
          <StatusTag :value="selected.status" />
          <el-select
            v-model="status"
            size="small"
            style="width: 150px"
            aria-label="Change cluster status"
            @change="changeStatus"
          >
            <el-option :value="selected.status" :label="selected.status" />
            <el-option v-for="s in next[selected.status] ?? []" :key="s" :value="s" :label="s" />
          </el-select>
          <el-button size="small" @click="editing = true">Edit</el-button>
        </template>
        <KeyValues
          :items="[
            ['Last health check', selected.lastHealthCheck ? when(selected.lastHealthCheck) : 'Not yet'],
            ['kubectl endpoint', selected.proxyEndpoint],
            ['Web console', selected.consoleUrl],
            ['App domain', '*.' + selected.appsDomain],
            ['Ranges the cluster uses', selected.reservedCidrs.join(', ') || 'None given'],
          ]"
        >
          <template #v1
            ><span class="mono">{{ selected.proxyEndpoint }}</span></template
          >
          <template #v2
            ><span class="mono">{{ selected.consoleUrl }}</span></template
          >
          <template #v3
            ><span class="mono">*.{{ selected.appsDomain }}</span></template
          >
          <template #v4
            ><span class="mono">{{ selected.reservedCidrs.join(', ') || 'None given' }}</span></template
          >
        </KeyValues>
        <p class="t-field-note" style="margin-top: 14px">
          In Maintenance, existing Workspaces keep running and stay reachable; creating and resizing are paused for
          everyone.
        </p>
      </TPanel>

      <TPanel title="Shared pools" flush>
        <template #extra><el-button size="small" @click="openPool()">Add pool</el-button></template>
        <ErrorState v-if="pools.isError.value" :error="pools.error.value" @retry="pools.refetch()" />
        <div v-else class="t-tablewrap">
          <el-table
            v-loading="pools.isPending.value"
            :data="pools.data.value ?? []"
            style="width: 100%"
            empty-text="No shared pool."
          >
            <el-table-column label="Pool" min-width="150">
              <template #default="{ row }"
                ><code>{{ row.id }}</code>
                <div class="muted small">{{ row.name }}</div></template
              >
            </el-table-column>
            <el-table-column label="Nodes" min-width="160">
              <template #default="{ row }"
                ><code v-for="n in row.nodes" :key="n" style="margin-right: 6px">{{ n }}</code
                ><span v-if="!row.nodes.length" class="muted">None</span></template
              >
            </el-table-column>
            <el-table-column label="Overcommit" width="110" align="right">
              <template #default="{ row }"
                ><span class="num">{{ row.allocationOvercommitRatio }}×</span></template
              >
            </el-table-column>
            <el-table-column label="CPU reserved" width="150" align="right">
              <template #default="{ row }"
                ><span class="num"
                  >{{ cores(row.capacity.allocated.cpu) }} of {{ cores(row.capacity.ceiling.cpu) }}</span
                ></template
              >
            </el-table-column>
            <el-table-column label="Memory reserved" width="170" align="right">
              <template #default="{ row }"
                ><span class="num"
                  >{{ gib(row.capacity.allocated.memory) }} of {{ gib(row.capacity.ceiling.memory) }} Gi</span
                ></template
              >
            </el-table-column>
            <el-table-column width="130" align="right">
              <template #default="{ row }"
                ><el-button link type="primary" @click="openPool(row)">Change…</el-button></template
              >
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <TPanel title="Dedicated nodes" flush>
        <template #extra
          ><el-button size="small" @click="giving = true">Give a node to an Organization</el-button></template
        >
        <ErrorState v-if="nodes.isError.value" :error="nodes.error.value" @retry="nodes.refetch()" />
        <div v-else class="t-tablewrap">
          <el-table
            v-loading="nodes.isPending.value"
            :data="dedicated"
            style="width: 100%"
            empty-text="No node is dedicated to an Organization."
          >
            <el-table-column label="Node" width="160">
              <template #default="{ row }"
                ><code>{{ row.name }}</code></template
              >
            </el-table-column>
            <el-table-column label="Organization" min-width="160">
              <template #default="{ row }">{{ row.dedicatedTo.displayName }}</template>
            </el-table-column>
            <el-table-column label="Node health" width="140">
              <template #default="{ row }"><StatusTag :value="row.ready ? 'Ready' : 'NotReady'" /></template>
            </el-table-column>
            <el-table-column label="Allocatable" min-width="170" align="right">
              <template #default="{ row }"
                ><span class="num"
                  >{{ cores(row.allocatable.cpu) }} cores, {{ gib(row.allocatable.memory) }} Gi</span
                ></template
              >
            </el-table-column>
            <el-table-column width="140" align="right">
              <template #default="{ row }"
                ><el-button link type="primary" @click="router.push(`/admin/organizations/${row.dedicatedTo.id}`)"
                  >Capacity</el-button
                ><el-button link type="danger" @click="takeBack(row)">Take back</el-button></template
              >
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <TPanel title="All nodes" flush>
        <div class="t-tablewrap">
          <el-table :data="nodes.data.value ?? []" style="width: 100%" empty-text="No node reported yet.">
            <el-table-column label="Node" width="170"
              ><template #default="{ row }"
                ><code>{{ row.name }}</code></template
              ></el-table-column
            >
            <el-table-column label="Kind" width="130" prop="kind" />
            <el-table-column label="Health" width="130"
              ><template #default="{ row }"><StatusTag :value="row.ready ? 'Ready' : 'NotReady'" /></template
            ></el-table-column>
            <el-table-column label="Used for" min-width="200">
              <template #default="{ row }"
                ><span v-if="row.sharedPoolId"
                  >Pool <code>{{ row.sharedPoolId }}</code></span
                ><span v-else-if="row.dedicatedTo">Dedicated to {{ row.dedicatedTo.displayName }}</span
                ><span v-else-if="row.kind !== 'Worker'" class="muted">Platform</span
                ><span v-else class="muted">Free</span></template
              >
            </el-table-column>
            <el-table-column label="Allocatable" min-width="170" align="right">
              <template #default="{ row }"
                ><span class="num"
                  >{{ cores(row.allocatable.cpu) }} cores, {{ gib(row.allocatable.memory) }} Gi,
                  {{ row.allocatable.pods }} pods</span
                ></template
              >
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <TPanel title="Pod network pools" flush>
        <template #extra><el-button size="small" @click="openNet()">Add pool</el-button></template>
        <ErrorState v-if="nets.isError.value" :error="nets.error.value" @retry="nets.refetch()" />
        <div v-else class="t-tablewrap">
          <el-table
            v-loading="nets.isPending.value"
            :data="nets.data.value ?? []"
            style="width: 100%"
            empty-text="No network pool."
          >
            <el-table-column label="Pool" min-width="200"
              ><template #default="{ row }"
                ><code>{{ row.id }}</code></template
              ></el-table-column
            >
            <el-table-column label="Ranges" min-width="180">
              <template #default="{ row }"
                ><span class="mono">{{ row.cidrs.join(', ') }}</span></template
              >
            </el-table-column>
            <el-table-column label="Belongs to" width="180">
              <template #default="{ row }">{{
                row.organizationId ? row.organizationId : 'Every Organization'
              }}</template>
            </el-table-column>
            <el-table-column label="Block sizes" width="150">
              <template #default="{ row }"
                ><span class="mono">{{
                  row.allowedPrefixLengths.map((p: number) => '/' + p).join(' ')
                }}</span></template
              >
            </el-table-column>
            <el-table-column width="110" align="right">
              <template #default="{ row }"
                ><el-button link type="primary" @click="openNet(row)">Change…</el-button></template
              >
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <ClusterDialog v-model="editing" :cluster="selected" />
      <SharedPoolDialog v-model="poolOpen" :cluster-id="clusterId" :nodes="nodes.data.value ?? []" :pool="poolEdit" />
      <GiveNodeDialog v-model="giving" :cluster-id="clusterId" :nodes="nodes.data.value ?? []" />
      <NetworkPoolDialog v-model="netOpen" :cluster-id="clusterId" :pool="netEdit" />
    </template>

    <ClusterDialog v-model="registering" @registered="pick" />
  </AppShell>
</template>
