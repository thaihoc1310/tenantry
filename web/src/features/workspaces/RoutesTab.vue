<script setup lang="ts">
// A Workspace's Routes (docs/design/mockups: Workspace, Routes): each puts one
// Service on the internet over HTTPS at <name>-<workspace>.<apps domain>
// (PRODUCT-PLAN §7.6). Owners create and delete them; a Route the controller
// could not build says why. Tenants cannot write Ingresses themselves.
import { ElMessage } from 'element-plus'
import { computed, reactive, ref } from 'vue'
import { ApiError, type Schemas } from '@/api/client'
import { showFailure } from '@/app/failure'
import { errorText } from '@/i18n/errors'
import CopyField from '@/components/CopyField.vue'
import EmptyState from '@/components/EmptyState.vue'
import NumField from '@/components/NumField.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import type { Workspace } from './queries'
import { useCreateRoute, useDeleteRoute, useWorkspaceNamespaces, useWorkspaceRoutes } from './queries'

const props = defineProps<{ ws: Workspace; owns: boolean; busy: string }>()
const routes = useWorkspaceRoutes(() => props.ws.id)
const namespaces = useWorkspaceNamespaces(() => props.ws.id)
const rows = computed(() => routes.data.value ?? [])
const active = computed(() => (namespaces.data.value ?? []).filter((n) => n.state === 'Active'))
const max = 20
const why = computed(() => {
  if (!props.owns) return 'Only members with the TenantOwner role can create and delete Routes.'
  if (props.busy) return props.busy
  if (rows.value.length >= max) return `The Workspace has ${max} Routes, as many as allowed.`
  if (!active.value.length) return 'Create a namespace first: a Route sends traffic to a Service in one.'
  return ''
})

// ---- create ----
const creating = ref(false)
const form = reactive({ namespace: '', name: '', service: '', port: 80 })
const errors = reactive<Record<string, string>>({})
const create = useCreateRoute()
const address = computed(
  () => `https://${form.name || 'name'}-${props.ws.k8sName}.${props.ws.appsDomain ?? '<apps domain>'}`,
)
function openCreate() {
  Object.assign(form, { namespace: active.value[0]?.name ?? '', name: '', service: '', port: 80 })
  for (const k of Object.keys(errors)) delete errors[k]
  creating.value = true
}
async function submit() {
  for (const k of Object.keys(errors)) delete errors[k]
  if (!/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(form.name)) errors.name = 'Lowercase letters, digits and hyphens.'
  else if (form.name.length + 1 + props.ws.k8sName.length > 63)
    errors.name = `At most ${62 - props.ws.k8sName.length} characters: ${form.name}-${props.ws.k8sName} must fit 63.`
  if (!/^[a-z]([-a-z0-9]*[a-z0-9])?$/.test(form.service)) errors.service = 'A Service name, starting with a letter.'
  if (Object.keys(errors).length) return
  try {
    await create.mutateAsync({ id: props.ws.id, body: { ...form } })
    ElMessage.success('Creating the Route: the cluster follows.')
    creating.value = false
  } catch (err) {
    if (err instanceof ApiError && err.field && ['name', 'service', 'namespace', 'port'].includes(err.field))
      errors[err.field] = `${errorText[err.code]?.en ?? ''} ${err.message} (${err.code})`.trim()
    else showFailure(err)
  }
}

// ---- delete ----
const doomed = ref<Schemas['WorkspaceRoute']>()
const del = useDeleteRoute()
async function remove() {
  if (!doomed.value) return
  try {
    await del.mutateAsync({ id: props.ws.id, routeId: doomed.value.id })
    ElMessage.success(`Taking ${doomed.value.hostname} down.`)
    doomed.value = undefined
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <TPanel title="Routes" :note="`${rows.length} of ${max}`" flush>
    <template #extra>
      <el-tooltip :content="why" :disabled="!why">
        <span
          ><el-button type="primary" :disabled="!!why" @click="openCreate"
            ><el-icon><Plus /></el-icon>&nbsp;Create Route</el-button
          ></span
        >
      </el-tooltip>
    </template>
    <el-skeleton v-if="routes.isPending.value" :rows="3" animated style="padding: 16px" />
    <EmptyState
      v-else-if="!rows.length"
      title="No Routes yet"
      description="A Route puts one Service on the internet over HTTPS, with a certificate handled for you."
    />
    <div v-else class="t-tablewrap">
      <el-table :data="rows" style="width: 100%">
        <el-table-column label="Address" min-width="320"
          ><template #default="{ row }"
            ><a v-if="row.state === 'Active'" :href="row.url" target="_blank" rel="noopener" class="mono">{{
              row.url
            }}</a
            ><span v-else class="mono muted">{{ row.url }}</span>
            <div v-if="row.message" class="t-field-note t-field-err">{{ row.message }}</div></template
          ></el-table-column
        >
        <el-table-column label="State" width="130"
          ><template #default="{ row }"><StatusTag :value="row.state" /></template
        ></el-table-column>
        <el-table-column label="Sends traffic to" min-width="240"
          ><template #default="{ row }"
            >Service <code>{{ row.service }}</code
            >:{{ row.port }} <span class="muted">in</span> <span class="mono">{{ row.namespace }}</span></template
          ></el-table-column
        >
        <el-table-column width="100" align="right"
          ><template #default="{ row }"
            ><el-button
              link
              type="danger"
              :disabled="!owns || !!busy || row.state === 'Deleting'"
              :aria-label="`Delete ${row.hostname}`"
              @click="doomed = row"
              >Delete</el-button
            ></template
          ></el-table-column
        >
      </el-table>
    </div>
    <div class="t-panel-foot">
      A Route only says the address exists: a Service with no running Pods answers 503. Ingress objects created with
      kubectl are refused.
    </div>
  </TPanel>

  <el-dialog v-model="creating" title="Create Route" width="540px">
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item label="Namespace" for="rt-ns" :error="errors.namespace">
        <el-select id="rt-ns" v-model="form.namespace" style="width: 100%">
          <el-option v-for="n in active" :key="n.name" :value="n.name" :label="n.name" />
        </el-select>
      </el-form-item>
      <el-form-item label="Route name" for="rt-name" :error="errors.name">
        <el-input id="rt-name" v-model="form.name" maxlength="62" />
        <div class="t-field-note">The first part of the address. It cannot be changed later.</div>
      </el-form-item>
      <div class="t-grid-form">
        <el-form-item label="Service" for="rt-svc" :error="errors.service">
          <el-input id="rt-svc" v-model="form.service" maxlength="63" />
        </el-form-item>
        <el-form-item label="Port" for="rt-port" :error="errors.port">
          <NumField id="rt-port" v-model="form.port" :min="1" :max="65535" />
        </el-form-item>
      </div>
      <div class="t-stack" style="gap: 6px">
        <span class="small muted">Your app will be at</span>
        <CopyField :value="address" />
      </div>
    </el-form>
    <template #footer>
      <el-button @click="creating = false">Cancel</el-button>
      <el-button type="primary" :loading="create.isPending.value" @click="submit">Create Route</el-button>
    </template>
  </el-dialog>

  <el-dialog
    :model-value="!!doomed"
    :title="`Delete ${doomed?.hostname ?? ''}`"
    width="480px"
    @update:model-value="(o: boolean) => !o && (doomed = undefined)"
  >
    <p>The address stops answering once its Ingress is gone. The Service and its Pods keep running.</p>
    <template #footer>
      <el-button @click="doomed = undefined">Cancel</el-button>
      <el-button type="danger" :loading="del.isPending.value" @click="remove">Delete Route</el-button>
    </template>
  </el-dialog>
</template>
