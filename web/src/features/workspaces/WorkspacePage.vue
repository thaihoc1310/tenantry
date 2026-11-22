<script setup lang="ts">
// One Workspace (docs/design/mockups: Workspace): Overview with its
// conditions and recent changes followed live, Resources with what its Pods
// use and a resize, Compute, Network, Namespaces, Access roles, Danger zone
// with cordon and delete (IMPLEMENTATION-PLAN M7 step 10, M10), Connect
// (M11). Routes come with M11 too.
import { ElMessage } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { cores, cpu, gib, memory } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { when } from '@/app/format'
import ConditionTimeline from '@/components/ConditionTimeline.vue'
import AccessRolesTab from './AccessRolesTab.vue'
import ComputeDetail from './ComputeDetail.vue'
import ConnectTab from './ConnectTab.vue'
import { kubeconfigURL, openConsole } from './connect'
import NamespacesTab from './NamespacesTab.vue'
import NetworkDetail from './NetworkDetail.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import NumField from '@/components/NumField.vue'
import PageHeader from '@/components/PageHeader.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useOrganization } from '@/features/organizations/queries'
import {
  useCordon,
  useDeleteWorkspace,
  useOperation,
  usePatchWorkspace,
  useRetry,
  useWorkspaceBySlug,
  useWorkspaceOperations,
  useWorkspaceUsage,
} from './queries'

const props = defineProps<{ orgSlug: string; wsSlug: string }>()
const route = useRoute()
const router = useRouter()

const org = useOrganization(() => props.orgSlug)
const manages = computed(() => ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''))
const { one, id, missing } = useWorkspaceBySlug(
  () => props.orgSlug,
  () => props.wsSlug,
)
const ws = computed(() => one.data.value)
const usage = useWorkspaceUsage(id)
const ops = useWorkspaceOperations(id)
// The newest operation, followed live while it runs.
const latest = computed(() => ops.data.value?.[0])
const live = useOperation(() => latest.value?.id)
const current = computed(() => live.data.value ?? latest.value)

const tabs = [
  ['overview', 'Overview'],
  ['resources', 'Resources'],
  ['compute', 'Compute'],
  ['network', 'Network'],
  ['namespaces', 'Namespaces'],
  ['members', 'Access roles'],
  ['access', 'Connect'],
  ['danger', 'Danger zone'],
] as const
const tab = computed(() => (route.query.tab as string) || 'overview')
const tabTo = (t: string) => ({ query: { tab: t } })

// Owners create Namespaces; the OrgOwner and OrgAdmins also change roles.
const ownsTenant = computed(() => manages.value || !!ws.value?.myRoles?.includes('TenantOwner'))
// kubectl and the console are of use once the cluster side exists.
const reachable = computed(() => !!ws.value && ['Ready', 'Degraded', 'Cordoned'].includes(ws.value.state))
const whyNot = computed(() =>
  ws.value?.state === 'Deleting' ? 'This Workspace is being deleted.' : 'Available when the Workspace is Ready.',
)
// Why a Namespace or role cannot change now; '' when it can (409 WORKSPACE_NOT_READY otherwise).
const busy = computed(() => {
  if (!ws.value) return ''
  if (!['Ready', 'Degraded'].includes(ws.value.state))
    return ws.value.state === 'Cordoned' ? 'The Workspace is cordoned.' : 'Available when the Workspace is Ready.'
  return current.value && ['Pending', 'Running'].includes(current.value.state)
    ? 'Another change is under way; try again when it is done.'
    : ''
})

// ---- overview ---------------------------------------------------------------
const conditions = computed(() =>
  (ws.value?.conditions ?? []).map((c) => ({
    ...c,
    when: c.lastTransitionTime ? when(c.lastTransitionTime) : '',
  })),
)
const trueCount = computed(() => conditions.value.filter((c) => c.status === 'True').length)

async function retry() {
  if (!current.value) return
  try {
    await useRetryRun.mutateAsync(current.value.id)
    ElMessage.success('Retrying.')
  } catch (err) {
    showFailure(err)
  }
}
const useRetryRun = useRetry()

// ---- resources ---------------------------------------------------------------
const metersOf = computed(() => {
  const unit: Record<string, [string, (q: string) => number, string]> = {
    requestsCpu: ['CPU requests', cores, 'cores'],
    requestsMemory: ['Memory requests', gib, 'Gi'],
    limitsCpu: ['CPU limits', cores, 'cores'],
    limitsMemory: ['Memory limits', gib, 'Gi'],
    pods: ['Pods', Number, ''],
    requestsStorage: ['Storage', gib, 'Gi'],
  }
  return (usage.data.value?.items ?? []).map((i) => {
    const [label, f, u] = unit[i.resource]!
    return { label, used: f(i.used), total: f(i.limit), unit: u }
  })
})

const resizing = ref(false)
const patch = usePatchWorkspace()
const form = reactive({ rcpu: 0, rmem: 0, lcpu: 0, lmem: 0, pods: 0, storage: 0, nsQuota: 1 })
const resizeError = ref('')
watch(resizing, (o) => {
  const r = ws.value?.resources
  if (!o || !r) return
  Object.assign(form, {
    rcpu: cores(r.requestsCpu),
    rmem: gib(r.requestsMemory),
    lcpu: cores(r.limitsCpu ?? r.requestsCpu),
    lmem: gib(r.limitsMemory ?? r.requestsMemory),
    pods: r.pods,
    storage: gib(r.requestsStorage),
    nsQuota: r.namespaceQuota,
  })
  resizeError.value = ''
})
async function resize() {
  if (!ws.value) return
  resizeError.value = ''
  try {
    await patch.mutateAsync({
      id: ws.value.id,
      body: {
        resources: {
          requestsCpu: cpu(form.rcpu),
          requestsMemory: memory(form.rmem),
          limitsCpu: cpu(form.lcpu),
          limitsMemory: memory(form.lmem),
          pods: form.pods,
          requestsStorage: memory(form.storage),
          namespaceQuota: form.nsQuota,
        },
      },
    })
    ElMessage.success('Resizing: the cluster follows.')
    resizing.value = false
  } catch (err) {
    if (err instanceof ApiError) resizeError.value = err.message
    else showFailure(err)
  }
}

// ---- danger zone --------------------------------------------------------------
const cordon = useCordon()
const del = useDeleteWorkspace()
const confirmName = ref('')
async function toggleCordon() {
  if (!ws.value) return
  const on = ws.value.state !== 'Cordoned'
  try {
    await cordon.mutateAsync({ id: ws.value.id, cordon: on })
    ElMessage.success(on ? 'Cordoning: the Workspace turns read-only.' : 'Uncordoning.')
  } catch (err) {
    showFailure(err)
  }
}
async function remove() {
  if (!ws.value) return
  try {
    await del.mutateAsync({ id: ws.value.id, confirm: confirmName.value })
    ElMessage.success(`Deleting ${ws.value.displayName}.`)
    void router.push({ query: { tab: 'overview' } })
  } catch (err) {
    showFailure(err)
  }
}

const usable = computed(() => ['Ready', 'Degraded'].includes(ws.value?.state ?? ''))
</script>

<template>
  <AppShell active="workspaces" :org-slug="orgSlug">
    <ErrorState v-if="one.isError.value" :error="one.error.value" @retry="one.refetch()" />
    <TPanel v-else-if="missing">
      <EmptyState title="No such Workspace" description="It does not exist, or you have no role in it." />
    </TPanel>
    <el-skeleton v-else-if="!ws" :rows="8" animated />
    <template v-else>
      <PageHeader
        :title="ws.displayName"
        :crumbs="[{ label: 'Workspaces', to: `/orgs/${orgSlug}/workspaces` }, { label: ws.displayName }]"
      >
        <template #tags
          ><code>{{ ws.k8sName }}</code
          ><StatusTag :value="ws.state"
        /></template>
        <template #actions>
          <el-tooltip :content="whyNot" :disabled="reachable">
            <span>
              <el-button v-if="reachable" tag="a" :href="kubeconfigURL(ws.id)" download
                ><el-icon><Download /></el-icon>&nbsp;Kubeconfig</el-button
              >
              <el-button v-else disabled
                ><el-icon><Download /></el-icon>&nbsp;Kubeconfig</el-button
              >
            </span>
          </el-tooltip>
          <el-tooltip :content="whyNot" :disabled="reachable">
            <span
              ><el-button type="primary" :disabled="!reachable" @click="openConsole(ws.id)"
                ><el-icon><Monitor /></el-icon>&nbsp;Open web console</el-button
              ></span
            >
          </el-tooltip>
        </template>
      </PageHeader>

      <nav class="t-tabs" aria-label="Workspace sections">
        <RouterLink v-for="[t, label] in tabs" :key="t" :to="tabTo(t)" :aria-current="tab === t ? 'page' : undefined">{{
          label
        }}</RouterLink>
      </nav>

      <!-- Overview -->
      <template v-if="tab === 'overview'">
        <el-alert
          v-if="ws.state === 'Failed' && current?.state === 'Failed'"
          type="error"
          :closable="false"
          show-icon
          title="The Workspace could not be created"
        >
          <div>{{ current.errorMessage }}</div>
          <div class="t-errcode" style="margin: 6px 0 10px">{{ current.errorCode }} · operation {{ current.id }}</div>
          <el-button v-if="manages" type="danger" size="small" :loading="useRetryRun.isPending.value" @click="retry"
            >Retry</el-button
          >
        </el-alert>
        <el-alert
          v-if="ws.state === 'Cordoned'"
          type="warning"
          :closable="false"
          show-icon
          title="This Workspace is cordoned: read-only"
          description="Running Pods keep running, but nobody can change anything with kubectl or the web console."
        />
        <el-alert
          v-if="ws.state === 'Deleting' && current?.state === 'Blocked'"
          type="error"
          :closable="false"
          show-icon
          title="Deleting is blocked"
        >
          <div>{{ current.errorMessage }}</div>
          <div style="margin-top: 6px">Wait, retry, or ask a Platform Admin to force the deletion.</div>
          <el-button v-if="manages" size="small" style="margin-top: 8px" @click="retry">Retry</el-button>
        </el-alert>

        <div class="t-split">
          <TPanel
            :title="['Provisioning', 'Failed'].includes(ws.state) ? 'Progress' : 'Health'"
            :note="conditions.length ? `${trueCount} of ${conditions.length} conditions are True` : ''"
          >
            <ConditionTimeline v-if="conditions.length" :conditions="conditions" />
            <p v-else class="muted">Waiting for the cluster to report on this Workspace.</p>
          </TPanel>
          <div class="t-stack" style="gap: 20px">
            <TPanel title="Summary">
              <KeyValues
                compact
                :items="[
                  ['Cluster', ws.clusterId],
                  ['Compute', `${ws.compute.mode} on ${ws.compute.sharedPoolId ?? ws.compute.dedicatedNodeGrantId}`],
                  ['Namespaces', `${usage.data.value?.namespaces.length ?? 0} of ${ws.resources.namespaceQuota}`],
                  ['Created', when(ws.createdAt)],
                ]"
              />
            </TPanel>
            <TPanel title="Recent changes" flush>
              <ul class="t-list">
                <li v-for="op in ops.data.value ?? []" :key="op.id">
                  <span class="mono">{{ op.type }}</span>
                  <StatusTag :value="op.id === current?.id ? current.state : op.state" />
                </li>
              </ul>
            </TPanel>
          </div>
        </div>
      </template>

      <!-- Resources -->
      <template v-else-if="tab === 'resources'">
        <TPanel title="Quota of this Workspace" note="used by running Pods, out of what the Workspace reserved">
          <template #extra>
            <el-tooltip
              :content="manages ? 'Available when the Workspace is Ready.' : 'Only owners and admins can resize.'"
              :disabled="manages && usable"
              ><span
                ><el-button :disabled="!manages || !usable" @click="resizing = true">Resize</el-button></span
              ></el-tooltip
            >
          </template>
          <div class="t-meters">
            <QuotaMeter v-for="m in metersOf" :key="m.label" v-bind="m" />
          </div>
        </TPanel>
        <TPanel title="By namespace" flush>
          <div class="t-tablewrap">
            <el-table :data="usage.data.value?.namespaces ?? []" style="width: 100%" empty-text="No Namespace yet.">
              <el-table-column label="Namespace" min-width="220"
                ><template #default="{ row }"
                  ><span class="mono">{{ row.name }}</span></template
                ></el-table-column
              >
              <el-table-column label="Pods" width="100" align="right"
                ><template #default="{ row }"
                  ><span class="num">{{ row.usage.pods ?? 0 }}</span></template
                ></el-table-column
              >
              <el-table-column label="CPU requests" width="140" align="right"
                ><template #default="{ row }"
                  ><span class="num">{{ row.usage['requests.cpu'] ?? 0 }}</span></template
                ></el-table-column
              >
              <el-table-column label="Memory requests" width="160" align="right"
                ><template #default="{ row }"
                  ><span class="num">{{ row.usage['requests.memory'] ?? 0 }}</span></template
                ></el-table-column
              >
            </el-table>
          </div>
        </TPanel>
        <el-dialog v-model="resizing" title="Resize Workspace" width="560px">
          <el-form label-position="top" @submit.prevent="resize">
            <div class="t-grid-form">
              <el-form-item label="CPU requests" for="rs-rcpu"
                ><NumField id="rs-rcpu" v-model="form.rcpu" :step="0.1" unit="cores"
              /></el-form-item>
              <el-form-item label="CPU limits" for="rs-lcpu"
                ><NumField id="rs-lcpu" v-model="form.lcpu" :step="0.1" unit="cores"
              /></el-form-item>
              <el-form-item label="Memory requests" for="rs-rmem"
                ><NumField id="rs-rmem" v-model="form.rmem" :step="0.5" unit="Gi"
              /></el-form-item>
              <el-form-item label="Memory limits" for="rs-lmem"
                ><NumField id="rs-lmem" v-model="form.lmem" :step="0.5" unit="Gi"
              /></el-form-item>
              <el-form-item label="Pods" for="rs-pods"
                ><NumField id="rs-pods" v-model="form.pods" :min="1"
              /></el-form-item>
              <el-form-item label="Storage" for="rs-storage"
                ><NumField id="rs-storage" v-model="form.storage" unit="Gi"
              /></el-form-item>
              <el-form-item label="Namespaces" for="rs-ns"
                ><NumField id="rs-ns" v-model="form.nsQuota" :min="1"
              /></el-form-item>
            </div>
            <div v-if="resizeError" class="t-field-note t-field-err">{{ resizeError }}</div>
            <p class="t-field-note">
              A larger quota comes out of the Organization's grant; a smaller one cannot go below what the Pods use.
            </p>
          </el-form>
          <template #footer>
            <el-button @click="resizing = false">Cancel</el-button>
            <el-button type="primary" :loading="patch.isPending.value" @click="resize">Resize</el-button>
          </template>
        </el-dialog>
      </template>

      <!-- Compute -->
      <template v-else-if="tab === 'compute'">
        <TPanel title="Where the Pods run">
          <ComputeDetail v-if="manages" :org-slug="orgSlug" :ws="ws" />
          <KeyValues
            v-else
            :items="[
              ['Mode', ws.compute.mode === 'Shared' ? 'Shared nodes' : 'Dedicated node'],
              ['Placement', ws.compute.sharedPoolId ?? ws.compute.dedicatedNodeGrantId ?? ''],
            ]"
          />
        </TPanel>
        <p class="t-explain">
          Pods of this Workspace can only be scheduled on its {{ ws.compute.mode === 'Shared' ? 'pool' : 'node' }}. A
          Pod that asks for another node is refused. The mode cannot be changed after creation.
        </p>
      </template>

      <!-- Network -->
      <template v-else-if="tab === 'network'">
        <TPanel title="Pod network">
          <NetworkDetail :org-slug="orgSlug" :ws="ws" :manages="manages" />
        </TPanel>
        <p class="t-explain">
          The address block cannot be changed while Pods are running. After the Workspace is deleted, the block stays
          unused for a while before it is given out again.
        </p>
      </template>

      <!-- Namespaces -->
      <template v-else-if="tab === 'namespaces'">
        <NamespacesTab :ws="ws" :owns="ownsTenant" :busy="busy" />
      </template>

      <!-- Access roles -->
      <template v-else-if="tab === 'members'">
        <AccessRolesTab :org-slug="orgSlug" :ws="ws" :manages="manages" :busy="busy" />
      </template>

      <!-- Connect -->
      <template v-else-if="tab === 'access'">
        <ConnectTab :ws="ws" :manages="manages" :usable="reachable" :why="whyNot" />
      </template>

      <!-- Danger zone -->
      <template v-else>
        <TPanel title="Cordon: make the Workspace read-only">
          <div class="t-row t-between">
            <p class="muted" style="max-width: 62ch">
              Running Pods keep running, but nobody can change anything with kubectl or the web console until you
              uncordon. Use it during an incident or an audit.
            </p>
            <el-button
              :disabled="!manages || !['Ready', 'Degraded', 'Cordoned'].includes(ws.state)"
              :loading="cordon.isPending.value"
              @click="toggleCordon"
              >{{ ws.state === 'Cordoned' ? 'Uncordon' : 'Cordon Workspace' }}</el-button
            >
          </div>
        </TPanel>
        <TPanel title="Delete this Workspace" danger>
          <div class="t-stack">
            <el-alert type="error" :closable="false" show-icon title="Deleting cannot be undone">
              <div>
                This removes right away, with no grace period,
                <strong>{{ usage.data.value?.namespaces.length ?? 0 }} namespaces</strong> with all their workloads and
                volumes. The quota goes back to the Organization's grant.
              </div>
            </el-alert>
            <el-form label-position="top" @submit.prevent="remove">
              <el-form-item :label="`Type ${ws.slug} to confirm`" for="del-confirm">
                <el-input id="del-confirm" v-model="confirmName" style="max-width: 320px" :disabled="!manages" />
              </el-form-item>
            </el-form>
            <div>
              <el-button
                type="danger"
                :disabled="!manages || confirmName !== ws.slug || ws.state === 'Deleting'"
                :loading="del.isPending.value"
                @click="remove"
                >Delete Workspace</el-button
              >
            </div>
          </div>
        </TPanel>
      </template>
    </template>
  </AppShell>
</template>
