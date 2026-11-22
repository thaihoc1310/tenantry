<script setup lang="ts">
// Add a shared pool or change one: its overcommit ratio, the system reserve
// of each node and which nodes it has (docs/design/mockups: AdminClusters,
// "Nodes of shared-general"; PRODUCT-PLAN §6.2). Only free nodes join; a node
// that leaves keeps its Pods until the Admin drains it.
import { ElMessage } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import { cores, cpu, gib, memory, quantity } from '@/api/units'
import { showFailure } from '@/app/failure'
import NumField from '@/components/NumField.vue'
import { type ClusterNode, type SharedPool, useCreateSharedPool, usePutSharedPool } from './queries'

const props = defineProps<{ clusterId: string; nodes: ClusterNode[]; pool?: SharedPool }>()
const open = defineModel<boolean>({ required: true })

const create = useCreateSharedPool()
const put = usePutSharedPool()
const form = reactive({ id: '', name: '', ratio: 3, cpu: 0.5, memory: 1.5, pods: 10, nodes: [] as string[] })
const errors = ref<Record<string, string>>({})

watch(open, (o) => {
  if (!o) return
  const p = props.pool
  Object.assign(form, {
    id: p?.id ?? '',
    name: p?.name ?? '',
    ratio: p?.allocationOvercommitRatio ?? 3,
    cpu: p ? cores(p.systemReserve.cpu) : 0.5,
    memory: p ? gib(p.systemReserve.memory) : 1.5,
    pods: p?.systemReserve.pods ?? 10,
    nodes: [...(p?.nodes ?? [])],
  })
  errors.value = {}
  create.newKey()
})

// why a node cannot join, or '' when it can (PRODUCT-PLAN §6.2).
function why(n: ClusterNode): string {
  if (props.pool && n.sharedPoolId === props.pool.id) return ''
  if (n.kind !== 'Worker') return `${n.kind} node`
  if (n.dedicatedTo) return `Dedicated to ${n.dedicatedTo.displayName}`
  if (n.sharedPoolId) return `In pool ${n.sharedPoolId}`
  if (!n.ready) return 'Not Ready'
  return ''
}

const removed = computed(() => (props.pool?.nodes ?? []).filter((n) => !form.nodes.includes(n)))
// What the pool offers after the change: the nodes' allocatable less the reserve, per node.
const after = computed(() => {
  let c = 0
  let m = 0
  for (const n of props.nodes.filter((x) => form.nodes.includes(x.name))) {
    c += Math.max(0, quantity(n.allocatable.cpu) - form.cpu)
    m += Math.max(0, quantity(n.allocatable.memory) / 2 ** 30 - form.memory)
  }
  return { cpu: Math.round(c * 1000) / 1000, memory: Math.round(m * 10) / 10 }
})

async function submit() {
  errors.value = {}
  const body = {
    name: form.name.trim(),
    allocationOvercommitRatio: form.ratio,
    systemReserve: { cpu: cpu(form.cpu), memory: memory(form.memory), pods: form.pods },
    nodes: form.nodes,
  }
  try {
    const saved = props.pool
      ? await put.mutateAsync({ clusterId: props.clusterId, poolId: props.pool.id, body })
      : await create.mutateAsync({ clusterId: props.clusterId, body: { id: form.id.trim(), ...body } })
    const follows = saved.operation ? ' The operator labels the nodes now (see Operations).' : ''
    ElMessage.success((props.pool ? 'Pool saved.' : `Pool ${form.id} added.`) + follows)
    open.value = false
  } catch (err) {
    if (err instanceof ApiError && err.field) errors.value = { [err.field]: err.message }
    else showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" :title="pool ? `Pool ${pool.id}` : 'Add shared pool'" width="640px">
    <el-form label-position="top" @submit.prevent="submit">
      <div class="t-grid-form">
        <el-form-item v-if="!pool" label="Id" for="sp-id" :error="errors.id">
          <el-input id="sp-id" v-model="form.id" placeholder="shared-gpu" maxlength="63" />
          <div class="t-field-note">
            Nodes get the label <code>tenantry.io/compute-pool=&lt;id&gt;</code>. Not <code>system</code>.
          </div>
        </el-form-item>
        <el-form-item label="Name" for="sp-name" :error="errors.name">
          <el-input id="sp-name" v-model="form.name" maxlength="80" />
        </el-form-item>
        <el-form-item label="Overcommit ratio" for="sp-ratio" :error="errors.allocationOvercommitRatio">
          <NumField id="sp-ratio" v-model="form.ratio" :min="1" :max="99.99" :step="0.1" unit="×" />
          <div class="t-field-note">Workspaces may reserve this many times what the nodes offer.</div>
        </el-form-item>
      </div>
      <h3 style="margin: 4px 0 8px">System reserve, on each node</h3>
      <div class="t-fields">
        <el-form-item label="CPU" for="sp-cpu"
          ><NumField id="sp-cpu" v-model="form.cpu" :step="0.1" unit="cores"
        /></el-form-item>
        <el-form-item label="Memory" for="sp-mem"
          ><NumField id="sp-mem" v-model="form.memory" :step="0.5" unit="Gi"
        /></el-form-item>
        <el-form-item label="Pods" for="sp-pods"><NumField id="sp-pods" v-model="form.pods" /></el-form-item>
      </div>
      <div v-if="errors.systemReserve" class="t-field-note t-field-err">{{ errors.systemReserve }}</div>
      <h3 style="margin: 12px 0 4px">Nodes</h3>
      <el-checkbox-group v-model="form.nodes" aria-label="Nodes of this pool">
        <ul class="t-list t-picklist">
          <li v-for="n in nodes" :key="n.name">
            <el-checkbox :value="n.name" :disabled="!!why(n)"
              ><code>{{ n.name }}</code></el-checkbox
            >
            <span class="muted small num"
              >{{ why(n) || (form.nodes.includes(n.name) ? 'In this pool' : 'Free') }} ·
              {{ cores(n.allocatable.cpu) }} cores, {{ gib(n.allocatable.memory) }} Gi</span
            >
          </li>
        </ul>
      </el-checkbox-group>
      <div v-if="errors.nodes" class="t-field-note t-field-err">{{ errors.nodes }}</div>
      <el-alert
        v-if="removed.length"
        type="warning"
        :closable="false"
        show-icon
        :title="`Pods on ${removed.join(', ')} keep running`"
        style="margin-top: 14px"
      >
        <div>Taking a node out only stops new Pods from landing on it. To empty the node, drain it yourself:</div>
        <div class="mono">kubectl drain {{ removed[0] }} --ignore-daemonsets --delete-emptydir-data</div>
      </el-alert>
      <p class="t-field-note" style="margin-top: 12px">
        After this change: {{ form.nodes.length }} nodes, {{ after.cpu }} cores and {{ after.memory }} Gi to reserve
        against; Workspaces may reserve up to {{ Math.round(after.cpu * form.ratio * 100) / 100 }} cores.
      </p>
    </el-form>
    <template #footer>
      <el-button @click="open = false">Cancel</el-button>
      <el-button
        type="primary"
        :loading="create.isPending.value || put.isPending.value"
        :disabled="!form.name.trim() || (!pool && !form.id.trim())"
        @click="submit"
        >{{ pool ? 'Save pool' : 'Add pool' }}</el-button
      >
    </template>
  </el-dialog>
</template>
