<script setup lang="ts">
// One Organization's grant on one cluster (docs/design/mockups: AdminGrant;
// PRODUCT-PLAN §5.5): a ceiling for what it may reserve from now on, which
// never touches Workspaces that already run. Before saving, the Admin sees
// what changes. A cluster the Organization has no grant on yet gets a new one.
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import GrantForm from './GrantForm.vue'
import { emptyGrant, fieldOf, fromGrant, type GrantModel, toApproval } from './grant'
import { useAdminOrganization, useDeleteGrant, useNetworkPools, usePutGrant, useSharedPools } from './queries'

const props = defineProps<{ orgId: string; clusterId: string }>()
const router = useRouter()

const detail = useAdminOrganization(() => props.orgId)
const put = usePutGrant()
const del = useDeleteGrant()
const pools = useSharedPools(() => props.clusterId)
const nets = useNetworkPools(() => props.clusterId)

const o = computed(() => detail.data.value?.organization)
const current = computed(() => detail.data.value?.clusterGrants.find((g) => g.clusterId === props.clusterId))
const usage = computed(() => detail.data.value?.usage?.clusterGrants.find((u) => u.grantId === current.value?.id))
const nodes = computed(() => (detail.data.value?.dedicatedNodes ?? []).filter((n) => n.clusterId === props.clusterId))

const grant = ref<GrantModel>(emptyGrant())
const errors = ref<Partial<Record<keyof GrantModel, string>>>({})
const reset = () => {
  grant.value = current.value ? fromGrant(current.value) : { ...emptyGrant(), clusterId: props.clusterId }
  errors.value = {}
}
watch(current, reset, { immediate: true })

const options = computed(() => ({
  pools: (pools.data.value ?? []).map((p) => p.id),
  nets: (nets.data.value ?? []).filter((n) => !n.organizationId || n.organizationId === props.orgId).map((n) => n.id),
  classes: grant.value.sc ? [grant.value.sc] : ['standard'],
}))
// What the Organization's Workspaces hold now, under each field.
const notes = computed<Partial<Record<keyof GrantModel, string>>>(() => {
  const u = usage.value
  if (!u) return {}
  return {
    ws: `Reserved now: ${u.workspaces.allocated}`,
    ns: `Reserved now: ${u.namespaces.allocated}`,
    storage: `Reserved now: ${gib(u.requestsStorage.allocated)} Gi`,
    rcpu: `Reserved now: ${cores(u.requestsCpu.allocated)}`,
    lcpu: `Reserved now: ${cores(u.limitsCpu.allocated)}`,
    rmem: `Reserved now: ${gib(u.requestsMemory.allocated)} Gi`,
    lmem: `Reserved now: ${gib(u.limitsMemory.allocated)} Gi`,
    pods: `Reserved now: ${u.pods.allocated}`,
  }
})

// The fields that change, before → after, for the confirmation.
const labels: [keyof GrantModel, string, string][] = [
  ['ws', 'Workspaces', ''],
  ['ns', 'Namespaces', ''],
  ['storage', 'Storage', ' Gi'],
  ['pool', 'Shared pool', ''],
  ['rcpu', 'Shared CPU requests', ' cores'],
  ['lcpu', 'Shared CPU limits', ' cores'],
  ['rmem', 'Shared memory requests', ' Gi'],
  ['lmem', 'Shared memory limits', ' Gi'],
  ['pods', 'Shared Pods', ''],
  ['net', 'Pod network pool', ''],
  ['sc', 'Storage class', ''],
]
const before = computed(() => (current.value ? fromGrant(current.value) : undefined))
const changes = computed(() =>
  labels
    .filter(([k]) => !before.value || before.value[k] !== grant.value[k])
    .map(([k, label, unit]) => [
      label,
      before.value ? `${before.value[k] || 'none'}${unit}` : '—',
      `${grant.value[k] || 'none'}${unit}`,
    ]),
)
const confirming = ref(false)

async function save() {
  errors.value = {}
  try {
    await put.mutateAsync({
      orgId: props.orgId,
      clusterId: props.clusterId,
      body: toApproval(grant.value).clusterGrant,
    })
    confirming.value = false
    ElMessage.success(current.value ? 'Grant saved.' : `${o.value?.displayName} has a grant on ${props.clusterId}.`)
  } catch (err) {
    confirming.value = false
    if (err instanceof ApiError && fieldOf[err.field]) errors.value = { [fieldOf[err.field]!]: err.message }
    else showFailure(err)
  }
}

async function remove() {
  try {
    await ElMessageBox.confirm(
      `${o.value?.displayName} can no longer create Workspaces on ${props.clusterId}.${current.value?.isDefault ? ' Its oldest other grant becomes the default.' : ''}`,
      'Remove this grant?',
      { type: 'warning', confirmButtonText: 'Remove grant', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  try {
    await del.mutateAsync({ orgId: props.orgId, clusterId: props.clusterId })
    ElMessage.success('Grant removed.')
    router.push(`/admin/organizations/${props.orgId}`)
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell area="admin" active="grants">
    <ErrorState v-if="detail.isError.value" :error="detail.error.value" @retry="detail.refetch()" />
    <el-skeleton v-else-if="!o" :rows="8" animated />
    <template v-else>
      <PageHeader
        :title="`Grant of ${o.displayName} on ${clusterId}`"
        :crumbs="[
          { label: 'Grants', to: '/admin/grants' },
          { label: o.displayName, to: `/admin/organizations/${orgId}` },
          { label: clusterId },
        ]"
        :description="`A grant is a ceiling for what ${o.displayName} may reserve from now on. Changing it never touches Workspaces that already run.`"
      >
        <template v-if="current?.isDefault" #tags
          ><el-tag type="info" effect="plain" round>Default cluster</el-tag></template
        >
      </PageHeader>

      <TPanel>
        <div class="t-stack" style="gap: 20px">
          <el-alert
            v-if="!current"
            type="info"
            :closable="false"
            show-icon
            :title="`${o.displayName} has no grant on ${clusterId} yet`"
            description="Saving creates it. The cluster must be Ready."
          />
          <GrantForm v-model="grant" :errors="errors" :options="options" :notes="notes" :limits="false" idp="gr" />
          <div class="t-row t-between">
            <el-tooltip
              :disabled="!nodes.length"
              :content="`${o.displayName} holds ${nodes.length} dedicated node(s) on ${clusterId}: take them back first.`"
            >
              <span
                ><el-button
                  v-if="current"
                  type="danger"
                  plain
                  :disabled="!!nodes.length"
                  :loading="del.isPending.value"
                  @click="remove"
                  >Remove grant</el-button
                ></span
              >
            </el-tooltip>
            <div class="t-row">
              <el-button :disabled="!changes.length" @click="reset">Discard changes</el-button>
              <el-button type="primary" :disabled="current ? !changes.length : !grant.net" @click="confirming = true"
                >Review changes</el-button
              >
            </div>
          </div>
        </div>
      </TPanel>

      <TPanel :title="`Dedicated nodes of ${o.displayName}`" flush>
        <template #extra><RouterLink to="/admin/clusters" class="small">Manage nodes</RouterLink></template>
        <ul v-if="nodes.length" class="t-list">
          <li v-for="n in nodes" :key="n.id">
            <span
              ><code>{{ n.name }}</code> &nbsp;<StatusTag :value="n.nodeState" /> <StatusTag :value="n.capacityState"
            /></span>
            <span class="num muted"
              >{{ cores(n.reserved.cpu) }} of {{ cores(n.reservable.cpu) }} cores, {{ gib(n.reserved.memory) }} of
              {{ gib(n.reservable.memory) }} Gi reserved</span
            >
          </li>
        </ul>
        <p v-else class="muted" style="padding: 16px 20px">
          No node of {{ clusterId }} is dedicated to {{ o.displayName }}.
        </p>
      </TPanel>

      <el-dialog v-model="confirming" :title="`Save these changes to ${o.displayName}'s grant?`" width="560px">
        <el-table :data="changes" style="width: 100%">
          <el-table-column label="Field" min-width="180"
            ><template #default="{ row }">{{ row[0] }}</template></el-table-column
          >
          <el-table-column label="Now" width="130"
            ><template #default="{ row }"
              ><span class="num muted">{{ row[1] }}</span></template
            ></el-table-column
          >
          <el-table-column label="After" width="130"
            ><template #default="{ row }"
              ><strong class="num">{{ row[2] }}</strong></template
            ></el-table-column
          >
        </el-table>
        <p class="t-explain" style="margin-top: 14px">
          Running Workspaces keep everything they have reserved. The new numbers decide what {{ o.displayName }} can
          create or grow from now on.
        </p>
        <template #footer>
          <el-button @click="confirming = false">Back</el-button>
          <el-button type="primary" :loading="put.isPending.value" @click="save">Save grant</el-button>
        </template>
      </el-dialog>
    </template>
  </AppShell>
</template>
