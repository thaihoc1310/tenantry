<script setup lang="ts">
// A Workspace's Namespaces (docs/design/mockups: Workspace, Namespaces): those
// asked for on the portal and those made with kubectl, what their Pods
// request, and for owners create and delete (PRODUCT-PLAN §4.5). Deleting one
// whose Pods hold volumes asks for its name, as everything in it goes.
import { ElMessage } from 'element-plus'
import { computed, ref } from 'vue'
import { ApiError } from '@/api/client'
import { gib, quantity } from '@/api/units'
import { showFailure } from '@/app/failure'
import EmptyState from '@/components/EmptyState.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import type { Schemas } from '@/api/client'
import type { Workspace } from './queries'
import { useCreateNamespace, useDeleteNamespace, useWorkspaceNamespaces } from './queries'

const props = defineProps<{ ws: Workspace; owns: boolean; busy: string }>()
const list = useWorkspaceNamespaces(() => props.ws.id)
const rows = computed(() => list.data.value ?? [])
const left = computed(() => props.ws.resources.namespaceQuota - rows.value.length)
const why = computed(() =>
  !props.owns
    ? 'Only members with the TenantOwner role can create and delete namespaces.'
    : props.busy || (left.value <= 0 ? `The Workspace holds ${props.ws.resources.namespaceQuota}, its limit.` : ''),
)

// ---- create ----
const creating = ref(false)
const name = ref('')
const nameError = ref('')
const create = useCreateNamespace()
function openCreate() {
  name.value = ''
  nameError.value = ''
  creating.value = true
}
async function submit() {
  nameError.value = ''
  if (!/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(name.value) || props.ws.k8sName.length + 1 + name.value.length > 63) {
    nameError.value = `Lowercase letters, digits and hyphens; ${props.ws.k8sName}-${name.value || 'name'} must fit 63 characters.`
    return
  }
  try {
    await create.mutateAsync({ id: props.ws.id, name: name.value })
    ElMessage.success('Creating the namespace: the cluster follows.')
    creating.value = false
  } catch (err) {
    if (err instanceof ApiError && err.status === 409 && err.code === 'CONFLICT') nameError.value = err.message
    else showFailure(err)
  }
}

// ---- delete ----
const doomed = ref<Schemas['WorkspaceNamespace']>()
const confirmName = ref('')
const del = useDeleteNamespace()
// Storage its PVCs request: volumes that go with the namespace.
const storage = (n: Schemas['WorkspaceNamespace']) => {
  const s = n.usage['requests.storage']
  return s ? gib(s) : 0
}
const needsName = computed(() => !!doomed.value && quantity(doomed.value.usage['requests.storage'] ?? '0') > 0)
function openDelete(n: Schemas['WorkspaceNamespace']) {
  doomed.value = n
  confirmName.value = ''
}
async function remove() {
  if (!doomed.value) return
  try {
    await del.mutateAsync({ id: props.ws.id, name: doomed.value.name })
    ElMessage.success(`Deleting ${doomed.value.name}.`)
    doomed.value = undefined
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <TPanel title="Namespaces" :note="`${rows.length} of ${ws.resources.namespaceQuota}`" flush>
    <template #extra>
      <el-tooltip :content="why" :disabled="!why">
        <span
          ><el-button type="primary" :disabled="!!why" @click="openCreate"
            ><el-icon><Plus /></el-icon>&nbsp;Create namespace</el-button
          ></span
        >
      </el-tooltip>
    </template>
    <el-skeleton v-if="list.isPending.value" :rows="3" animated style="padding: 16px" />
    <EmptyState
      v-else-if="!rows.length"
      title="No namespaces yet"
      description="Owners create namespaces here or with kubectl; Pods run in them."
    />
    <div v-else class="t-tablewrap">
      <el-table :data="rows" style="width: 100%">
        <el-table-column label="Namespace" min-width="220"
          ><template #default="{ row }"
            ><span class="mono">{{ row.name }}</span></template
          ></el-table-column
        >
        <el-table-column label="State" width="110"
          ><template #default="{ row }"><StatusTag :value="row.state" /></template
        ></el-table-column>
        <el-table-column label="Created with" width="110"
          ><template #default="{ row }">{{
            row.source === 'kubectl' ? 'kubectl' : 'Portal'
          }}</template></el-table-column
        >
        <el-table-column label="Pods" width="70" align="right"
          ><template #default="{ row }"
            ><span class="num">{{ row.usage['pods'] ?? 0 }}</span></template
          ></el-table-column
        >
        <el-table-column label="Unschedulable" width="120" align="right"
          ><template #default="{ row }"
            ><el-tag v-if="row.unschedulablePods" type="warning" size="small" effect="plain" round>{{
              row.unschedulablePods
            }}</el-tag
            ><span v-else class="num muted">0</span></template
          ></el-table-column
        >
        <el-table-column label="CPU req." width="90" align="right"
          ><template #default="{ row }"
            ><span class="num">{{ row.usage['requests.cpu'] ?? 0 }}</span></template
          ></el-table-column
        >
        <el-table-column label="Memory req." width="110" align="right"
          ><template #default="{ row }"
            ><span class="num">{{ row.usage['requests.memory'] ?? 0 }}</span></template
          ></el-table-column
        >
        <el-table-column label="Storage" width="90" align="right"
          ><template #default="{ row }"
            ><span class="num">{{ storage(row) }} Gi</span></template
          ></el-table-column
        >
        <el-table-column width="90" align="right"
          ><template #default="{ row }"
            ><el-button
              link
              type="danger"
              :disabled="!owns || !!busy || row.state === 'Deleting'"
              :aria-label="`Delete ${row.name}`"
              @click="openDelete(row)"
              >Delete</el-button
            ></template
          ></el-table-column
        >
      </el-table>
    </div>
    <div class="t-panel-foot">
      Namespaces created with kubectl appear here too. Deleting a namespace deletes everything in it, including its
      Routes and volumes.
    </div>
  </TPanel>

  <el-dialog v-model="creating" title="Create namespace" width="460px">
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item label="Name" for="ns-name" :error="nameError">
        <el-input id="ns-name" v-model="name" placeholder="staging" maxlength="42"
          ><template #prepend>{{ ws.k8sName }}-</template></el-input
        >
        <div class="t-field-note">
          Lowercase letters, digits and hyphens. {{ left }} of {{ ws.resources.namespaceQuota }} namespaces left.
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="creating = false">Cancel</el-button>
      <el-button type="primary" :loading="create.isPending.value" @click="submit">Create namespace</el-button>
    </template>
  </el-dialog>

  <el-dialog
    :model-value="!!doomed"
    :title="`Delete ${doomed?.name ?? ''}`"
    width="480px"
    @update:model-value="(o: boolean) => !o && (doomed = undefined)"
  >
    <template v-if="doomed">
      <p>Everything in the namespace is deleted: its Pods, Routes, Secrets and volumes. This cannot be undone.</p>
      <template v-if="needsName">
        <el-alert
          type="warning"
          :closable="false"
          show-icon
          :title="`Its volumes hold ${storage(doomed)} Gi of data, deleted with it.`"
          style="margin-bottom: 12px"
        />
        <el-form label-position="top" @submit.prevent="remove">
          <el-form-item :label="`Type ${doomed.name} to confirm`" for="ns-confirm">
            <el-input id="ns-confirm" v-model="confirmName" />
          </el-form-item>
        </el-form>
      </template>
    </template>
    <template #footer>
      <el-button @click="doomed = undefined">Cancel</el-button>
      <el-button
        type="danger"
        :disabled="needsName && confirmName !== doomed?.name"
        :loading="del.isPending.value"
        @click="remove"
        >Delete namespace</el-button
      >
    </template>
  </el-dialog>
</template>
