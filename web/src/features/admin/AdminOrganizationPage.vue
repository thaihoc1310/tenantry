<script setup lang="ts">
// One Organization as a Platform Admin sees it (PRODUCT-PLAN §16.5): its
// limits, which the Admin may change, its grants on each cluster, and the
// nodes dedicated to it. The numbers are those of the Organization's own
// /usage.
import { ElMessage } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { when } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import NumField from '@/components/NumField.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useAdminOrganization, useClusters, useDeleteOrganization, useSetEntitlement } from './queries'

const props = defineProps<{ orgId: string }>()
const router = useRouter()

const detail = useAdminOrganization(() => props.orgId)
const clusters = useClusters()
const save = useSetEntitlement()
const d = computed(() => detail.data.value)
const o = computed(() => d.value?.organization)

type Limit = 'maxMembers' | 'maxGroups' | 'maxWorkspacesTotal'
const limits: [Limit, string, 'members' | 'groups' | 'workspaces'][] = [
  ['maxMembers', 'Members', 'members'],
  ['maxGroups', 'Groups', 'groups'],
  ['maxWorkspacesTotal', 'Workspaces, all clusters', 'workspaces'],
]
const ent = reactive({ maxMembers: 0, maxGroups: 0, maxWorkspacesTotal: 0 })
const entError = ref<Partial<Record<Limit, string>>>({})
watch(
  () => d.value?.entitlement,
  (e) => {
    if (e) Object.assign(ent, e)
  },
  { immediate: true },
)
const changed = computed(() => {
  const e = d.value?.entitlement
  return !!e && limits.some(([k]) => e[k] !== ent[k])
})
const used = (k: 'members' | 'groups' | 'workspaces') => d.value?.usage?.organization[k].used ?? 0

async function saveLimits() {
  entError.value = {}
  try {
    await save.mutateAsync({ orgId: props.orgId, entitlement: { ...ent } })
    ElMessage.success('Limits saved.')
  } catch (err) {
    if (err instanceof ApiError && err.code === 'ENTITLEMENT_BELOW_USAGE') {
      const limit = err.message.split(' ')[0] as Limit
      entError.value = { [limit]: `${err.message} (ENTITLEMENT_BELOW_USAGE)` }
    } else showFailure(err)
  }
}

const grantRows = computed(() =>
  (d.value?.clusterGrants ?? []).map((g) => ({ g, u: d.value?.usage?.clusterGrants.find((u) => u.grantId === g.id) })),
)
const otherClusters = computed(() =>
  (clusters.data.value ?? []).filter((c) => !(d.value?.clusterGrants ?? []).some((g) => g.clusterId === c.id)),
)
const newCluster = ref('')

// Deleting an empty organization whose owner cannot (gone, or a test
// fixture): the owner's checks, ORGANIZATION_NOT_EMPTY says what is left.
const del = useDeleteOrganization()
const confirmSlug = ref('')
async function remove() {
  if (!o.value) return
  try {
    await del.mutateAsync(props.orgId)
    ElMessage.success(`${o.value.displayName} is deleted. Its groups leave the login system within a minute.`)
    router.push('/admin/organizations')
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell area="admin" active="organizations">
    <ErrorState v-if="detail.isError.value" :error="detail.error.value" @retry="detail.refetch()" />
    <el-skeleton v-else-if="!o" :rows="8" animated />

    <template v-else>
      <PageHeader
        :title="o.displayName"
        :crumbs="[{ label: 'Organizations', to: '/admin/organizations' }, { label: o.displayName }]"
      >
        <template #tags
          ><code>{{ o.slug }}</code
          ><StatusTag :value="o.status"
        /></template>
      </PageHeader>

      <TPanel title="Organization">
        <KeyValues
          :items="[
            ['Owner', o.owner.email ?? o.owner.displayName],
            ['Description', o.description || 'None'],
            ['Created', when(o.createdAt)],
          ]"
        >
          <template #v0
            >{{ o.owner.email ?? o.owner.displayName }}
            <RouterLink class="small" :to="{ path: '/admin/users', query: { q: o.owner.email ?? '' } }"
              >Account</RouterLink
            ></template
          >
        </KeyValues>
      </TPanel>

      <TPanel v-if="!d?.entitlement">
        <EmptyState
          title="Not active"
          description="A request under review or rejected has no limits, grants or usage yet."
        />
      </TPanel>

      <template v-else>
        <TPanel title="Limits" note="every cluster">
          <el-form label-position="top" @submit.prevent="saveLimits">
            <div class="t-fields">
              <el-form-item v-for="[k, label, u] in limits" :key="k" :label="label" :for="'ent-' + k">
                <NumField :id="'ent-' + k" v-model="ent[k]" />
                <div v-if="entError[k]" class="t-field-note t-field-err">{{ entError[k] }}</div>
                <div v-else class="t-field-note num">
                  Used now: {{ used(u) }}<template v-if="k === 'maxGroups'">, at least 3</template>
                </div>
              </el-form-item>
            </div>
            <div class="t-row" style="justify-content: flex-end">
              <el-button :disabled="!changed" @click="Object.assign(ent, d!.entitlement!)">Discard changes</el-button>
              <el-button type="primary" :disabled="!changed" :loading="save.isPending.value" @click="saveLimits"
                >Save limits</el-button
              >
            </div>
          </el-form>
        </TPanel>

        <TPanel title="Grants" flush>
          <template #extra>
            <el-select
              v-model="newCluster"
              size="small"
              placeholder="Grant another cluster"
              style="width: 200px"
              aria-label="Cluster to grant"
              :disabled="!otherClusters.length"
            >
              <el-option
                v-for="c in otherClusters"
                :key="c.id"
                :value="c.id"
                :label="`${c.displayName} (${c.status})`"
              />
            </el-select>
            <el-button size="small" :disabled="!newCluster" @click="router.push(`/admin/grants/${orgId}/${newCluster}`)"
              >Add grant</el-button
            >
          </template>
          <div class="t-tablewrap">
            <el-table :data="grantRows" style="width: 100%" empty-text="No grant.">
              <el-table-column label="Cluster" min-width="140">
                <template #default="{ row }"
                  ><code>{{ row.g.clusterId }}</code>
                  <el-tag v-if="row.g.isDefault" size="small" type="info" effect="plain" round
                    >Default</el-tag
                  ></template
                >
              </el-table-column>
              <el-table-column label="Workspaces" width="120" align="right">
                <template #default="{ row }"
                  ><span class="num"
                    >{{ row.u?.workspaces.allocated ?? 0 }} of {{ row.g.maxWorkspaces }}</span
                  ></template
                >
              </el-table-column>
              <el-table-column label="Shared CPU" width="130" align="right">
                <template #default="{ row }"
                  ><span class="num"
                    >{{ cores(row.u?.requestsCpu.allocated ?? '0') }} of {{ cores(row.g.resources.requestsCpu) }}</span
                  ></template
                >
              </el-table-column>
              <el-table-column label="Shared memory" width="150" align="right">
                <template #default="{ row }"
                  ><span class="num"
                    >{{ gib(row.u?.requestsMemory.allocated ?? '0') }} of
                    {{ gib(row.g.resources.requestsMemory) }} Gi</span
                  ></template
                >
              </el-table-column>
              <el-table-column label="Pools" min-width="200">
                <template #default="{ row }"
                  ><code
                    v-for="p in [...row.g.sharedPoolIds, ...row.g.networkPoolIds]"
                    :key="p"
                    style="margin-right: 6px"
                    >{{ p }}</code
                  ></template
                >
              </el-table-column>
              <el-table-column width="90" align="right">
                <template #default="{ row }"
                  ><el-button link type="primary" @click="router.push(`/admin/grants/${orgId}/${row.g.clusterId}`)"
                    >Edit</el-button
                  ></template
                >
              </el-table-column>
            </el-table>
          </div>
        </TPanel>

        <TPanel title="Dedicated nodes" flush>
          <template #extra
            ><RouterLink to="/admin/clusters" class="small">Give or take back nodes</RouterLink></template
          >
          <ul v-if="d?.dedicatedNodes?.length" class="t-list">
            <li v-for="n in d.dedicatedNodes" :key="n.id">
              <span
                ><code>{{ n.name }}</code> <span class="muted small">on {{ n.clusterId }}</span> &nbsp;<StatusTag
                  :value="n.nodeState" />
                <StatusTag :value="n.capacityState"
              /></span>
              <span class="num muted"
                >{{ cores(n.reserved.cpu) }} of {{ cores(n.reservable.cpu) }} cores, {{ gib(n.reserved.memory) }} of
                {{ gib(n.reservable.memory) }} Gi reserved</span
              >
            </li>
          </ul>
          <EmptyState
            v-else
            title="No dedicated node"
            description="Give this Organization a node on the Clusters page."
          />
        </TPanel>

        <TPanel title="Delete this Organization" note="cannot be undone">
          <p class="muted">
            For an Organization its owner cannot delete. It must be empty: no Workspace, none whose address block is
            still in quarantine, and no dedicated node. Its grants go, open invitations end and its groups leave the
            login system; the slug can be requested again.
          </p>
          <el-form label-position="top" @submit.prevent="remove">
            <el-form-item :label="`Type ${o.slug} to confirm`" for="org-del-confirm">
              <el-input id="org-del-confirm" v-model="confirmSlug" style="max-width: 320px" />
            </el-form-item>
            <el-button type="danger" :disabled="confirmSlug !== o.slug" :loading="del.isPending.value" @click="remove"
              >Delete Organization</el-button
            >
          </el-form>
        </TPanel>
      </template>
    </template>
  </AppShell>
</template>
