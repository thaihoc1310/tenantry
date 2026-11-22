<script setup lang="ts">
// Give a free node of a cluster to an Organization (docs/design/mockups:
// AdminClusters, "Give a node"; PRODUCT-PLAN §6.3). In M5 only the database
// changes: the operator checks the node and labels it from M8, so the node
// stays NotReady until then.
import { ElMessage } from 'element-plus'
import { computed, reactive, watch } from 'vue'
import { cores, cpu, gib, memory } from '@/api/units'
import { showFailure } from '@/app/failure'
import NumField from '@/components/NumField.vue'
import { type ClusterNode, useAllGrants, useChangeDedicatedNodes } from './queries'

const props = defineProps<{ clusterId: string; nodes: ClusterNode[] }>()
const open = defineModel<boolean>({ required: true })

const grants = useAllGrants(() => props.clusterId)
const give = useChangeDedicatedNodes()
const form = reactive({ node: '', orgId: '', cpu: 0.5, memory: 1.5, pods: 10 })

watch(open, (o) => {
  if (o) Object.assign(form, { node: '', orgId: '', cpu: 0.5, memory: 1.5, pods: 10 })
})

// Free: a worker in no pool and dedicated to nobody.
const free = computed(() => props.nodes.filter((n) => n.kind === 'Worker' && !n.sharedPoolId && !n.dedicatedTo))
// Organizations with a grant on the cluster: a node goes under the grant.
const orgs = computed(() => grants.items.value.map((g) => g.organization))
const chosen = computed(() => free.value.find((n) => n.name === form.node))

async function submit() {
  const org = orgs.value.find((o) => o.id === form.orgId)
  try {
    await give.change(form.orgId, props.clusterId, (nodes) => [
      ...nodes,
      { name: form.node, systemReserve: { cpu: cpu(form.cpu), memory: memory(form.memory), pods: form.pods } },
    ])
    ElMessage.success(`${form.node} is dedicated to ${org?.displayName}.`)
    open.value = false
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" title="Give a node to an Organization" width="600px">
    <el-form label-position="top" @submit.prevent="submit">
      <div class="t-grid-form">
        <el-form-item label="Node" for="ng-node">
          <el-select
            id="ng-node"
            v-model="form.node"
            style="width: 100%"
            placeholder="A free node"
            no-data-text="No free node"
          >
            <el-option
              v-for="n in free"
              :key="n.name"
              :value="n.name"
              :label="`${n.name} · ${cores(n.allocatable.cpu)} cores, ${gib(n.allocatable.memory)} Gi`"
            />
          </el-select>
          <div class="t-field-note">
            Only free nodes are listed. To use a node of a shared pool, take it out of the pool first.
          </div>
        </el-form-item>
        <el-form-item label="Organization" for="ng-org">
          <el-select
            id="ng-org"
            v-model="form.orgId"
            filterable
            style="width: 100%"
            placeholder="With a grant on this cluster"
            no-data-text="No Organization has a grant on this cluster"
          >
            <el-option v-for="o in orgs" :key="o.id" :value="o.id" :label="`${o.displayName} (${o.slug})`" />
          </el-select>
        </el-form-item>
      </div>
      <h3 style="margin: 4px 0 8px">System reserve of the node</h3>
      <div class="t-fields">
        <el-form-item label="CPU" for="ng-cpu"
          ><NumField id="ng-cpu" v-model="form.cpu" :step="0.1" unit="cores"
        /></el-form-item>
        <el-form-item label="Memory" for="ng-mem"
          ><NumField id="ng-mem" v-model="form.memory" :step="0.5" unit="Gi"
        /></el-form-item>
        <el-form-item label="Pods" for="ng-pods"><NumField id="ng-pods" v-model="form.pods" /></el-form-item>
      </div>
      <p v-if="chosen" class="t-field-note num">
        The Organization's Workspaces may reserve {{ Math.max(0, cores(chosen.allocatable.cpu) - form.cpu) }} cores and
        {{ Math.max(0, Math.round((gib(chosen.allocatable.memory) - form.memory) * 10) / 10) }} Gi on it.
      </p>
      <p class="t-explain">
        The node is checked for Pods of other tenants before it gets the label and taint that keep it for this
        Organization. Until then it shows as Not ready.
      </p>
    </el-form>
    <template #footer>
      <el-button @click="open = false">Cancel</el-button>
      <el-button type="primary" :loading="give.isPending.value" :disabled="!form.node || !form.orgId" @click="submit"
        >Give node</el-button
      >
    </template>
  </el-dialog>
</template>
