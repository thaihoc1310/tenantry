<script setup lang="ts">
// Create Workspace (docs/design/mockups: Wizard; PRODUCT-PLAN §19.1): seven
// steps over one form held by useWorkspaceWizard. Each forward move asks the
// server to validate the whole form; Create sends it with an Idempotency-Key
// and opens the new Workspace, whose page follows the create live.
import { computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import NumField from '@/components/NumField.vue'
import PageHeader from '@/components/PageHeader.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import ResourceGrid from '@/components/ResourceGrid.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import {
  useOrgClusters,
  useOrgComputeOptions,
  useOrgDedicatedNodes,
  useOrgNetworkPools,
  useOrgSharedPools,
  useOrganization,
  useUsage,
} from '@/features/organizations/queries'
import { useGroups } from '@/features/people/queries'
import { useCreateWorkspace, useNetworkAvailability, validateWorkspace } from './queries'
import { fill, nodeUsable, steps, toCreate, useWorkspaceWizard } from './wizard'

const props = defineProps<{ orgSlug: string }>()
const router = useRouter()

const org = useOrganization(() => props.orgSlug)
const manages = computed(() => ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''))
const active = computed(() => org.data.value?.status === 'Active')
const allowed = computed(() => manages.value && active.value)
const orgName = computed(() => org.data.value?.displayName ?? 'the Organization')

const w = useWorkspaceWizard(
  () => props.orgSlug,
  (body) => validateWorkspace(props.orgSlug, body),
)
const f = w.form

const clusters = useOrgClusters(() => props.orgSlug, allowed)
const clusterId = computed(() => (allowed.value ? f.clusterId : ''))
const modes = useOrgComputeOptions(() => props.orgSlug, clusterId)
const pools = useOrgSharedPools(() => props.orgSlug, clusterId)
const nodes = useOrgDedicatedNodes(() => props.orgSlug, clusterId)
const nets = useOrgNetworkPools(() => props.orgSlug, clusterId)
const usage = useUsage(() => props.orgSlug, allowed)
const groups = useGroups(() => props.orgSlug, allowed)
const availability = useNetworkAvailability(
  () => props.orgSlug,
  () => f.networkPoolId || undefined,
)

// Another cluster: its pools and nodes are other ones.
watch(
  () => f.clusterId,
  (now, before) => {
    if (before) Object.assign(f, { sharedPoolId: '', nodeGrantId: '', networkPoolId: '', prefix: 0, cidr: '' })
  },
)
watch(
  () => [clusters.data.value, modes.data.value, pools.data.value, nodes.data.value, nets.data.value, groups.data.value],
  () =>
    fill(f, {
      clusters: clusters.data.value ?? [],
      computeOptions: modes.data.value,
      sharedPools: pools.data.value,
      nodes: nodes.data.value,
      networkPools: nets.data.value,
      groups: groups.data.value,
    }),
  { immediate: true },
)

const dedicated = computed(() => f.mode === 'Dedicated')
const tenant = computed(() => `${props.orgSlug}-${f.slug || 'slug'}`)
const cluster = computed(() => clusters.data.value?.find((c) => c.id === f.clusterId))
const notReady = computed(() => clusters.data.value?.find((c) => c.isDefault && c.status !== 'Ready'))
const pool = computed(() => pools.data.value?.find((p) => p.id === f.sharedPoolId))
const node = computed(() => nodes.data.value?.find((n) => n.id === f.nodeGrantId))
const net = computed(() => nets.data.value?.find((n) => n.id === f.networkPoolId))
const grant = computed(() => usage.data.value?.clusterGrants.find((g) => g.clusterId === f.clusterId))
const modeOf = (m: string) => modes.data.value?.find((o) => o.mode === m)
const groupName = (id: string) => groups.data.value?.find((g) => g.id === id)?.name ?? id

// What is left where the numbers go: on the node for Dedicated, in the grant for Shared.
const left = computed(() => {
  const g = grant.value
  if (dedicated.value) {
    const n = node.value
    return {
      rcpu: n ? cores(n.remaining.cpu) : undefined,
      rmem: n ? gib(n.remaining.memory) : undefined,
      pods: n?.remaining.pods,
      where: n ? `on ${n.name}` : '',
    }
  }
  return {
    rcpu: g ? cores(g.requestsCpu.remaining) : undefined,
    rmem: g ? gib(g.requestsMemory.remaining) : undefined,
    lcpu: g ? cores(g.limitsCpu.remaining) : undefined,
    lmem: g ? gib(g.limitsMemory.remaining) : undefined,
    pods: g?.pods.remaining,
    where: 'in the shared quota',
  }
})
const notes = computed(() => {
  const l = left.value as Record<string, number | string | undefined>
  const out: Record<string, string> = {}
  for (const k of ['rcpu', 'rmem', 'lcpu', 'lmem']) if (l[k] !== undefined) out[k] = `${l[k]} left ${l.where}`
  return out
})
// The API's field names for ResourceGrid's keys.
const gridErrors = computed(() => ({
  rcpu: w.errors.value['resources.requestsCpu'],
  rmem: w.errors.value['resources.requestsMemory'],
  lcpu: w.errors.value['resources.limitsCpu'],
  lmem: w.errors.value['resources.limitsMemory'],
}))
const err = (field: string) => w.errors.value[field] ?? ''
// In Auto, the network fields are not on screen: their errors show under the summary.
const netError = computed(() =>
  Object.entries(w.errors.value)
    .filter(([k]) => k.startsWith('network'))
    .map(([, v]) => v)
    .join(' '),
)

const prefix = computed(() => (w.form.netAuto || !f.prefix ? (net.value?.defaultPrefixLength ?? 24) : f.prefix))
const addresses = (bits: number) => Math.max(2 ** (32 - bits) - 2, 0)
const freeAt = (bits: number) => availability.data.value?.free.find((x) => x.prefixLength === bits)?.blocks

const groupOff = (g: { id: string; syncStatus: string }, mine: string[]) =>
  g.syncStatus !== 'Synced' || ([...f.owners, ...f.devs, ...f.viewers].includes(g.id) && !mine.includes(g.id))
const groupLabel = (g: { name: string; syncStatus: string }) =>
  g.name + (g.syncStatus !== 'Synced' ? '  (sync pending)' : '')

const cls = (i: number) => ({
  done: w.visited[i] && i !== w.step.value && !w.hasError(i),
  err: w.marked(i),
})
const mark = (i: number) => (w.visited[i] && i !== w.step.value ? (w.hasError(i) ? '!' : '✓') : String(i + 1))
const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

async function move(to: () => Promise<boolean>) {
  try {
    await to()
    window.scrollTo(0, 0)
  } catch (e) {
    showFailure(e)
  }
}

const create = useCreateWorkspace(() => props.orgSlug)
async function submit() {
  try {
    const r = await create.mutateAsync(toCreate(f))
    await router.push(`/orgs/${props.orgSlug}/workspaces/${r.workspace.slug}`)
  } catch (e) {
    if (e instanceof ApiError && e.status !== 0 && e.code !== 'INTERNAL') w.place(e)
    else showFailure(e)
  }
}
</script>

<template>
  <AppShell active="workspaces" :org-slug="orgSlug">
    <PageHeader
      title="Create Workspace"
      :crumbs="[{ label: 'Workspaces', to: `/orgs/${orgSlug}/workspaces` }, { label: 'Create' }]"
    />
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="org.isPending.value" :rows="6" animated />
    <TPanel v-else-if="!allowed">
      <EmptyState
        :title="active ? 'Only owners and admins create Workspaces' : 'This Organization is not approved yet'"
        :description="
          active
            ? 'Ask the owner or an admin of the Organization to create one, or to bind one of your groups to a Workspace.'
            : 'Workspaces can be created once a Platform Admin approves it.'
        "
      />
    </TPanel>

    <div v-else class="t-wizard">
      <ol class="t-wsteps" aria-label="Steps">
        <li v-for="(s, i) in steps" :key="s">
          <button
            type="button"
            class="t-wstep"
            :class="cls(i)"
            :aria-current="i === w.step.value ? 'step' : undefined"
            @click="move(() => w.go(i))"
          >
            <span class="t-wstep-n">{{ mark(i) }}</span
            >{{ s }}
          </button>
        </li>
      </ol>

      <section class="t-panel">
        <div class="t-panel-head">
          <div class="t-row">
            <h2>{{ steps[w.step.value] }}</h2>
            <span class="muted">Step {{ w.step.value + 1 }} of {{ steps.length }}</span>
          </div>
        </div>

        <div class="t-panel-body t-stack" style="gap: 20px">
          <el-alert v-if="w.general.value" type="error" :closable="false" show-icon :title="w.general.value" />

          <!-- 1 Name -->
          <el-form v-if="w.step.value === 0" label-position="top" style="max-width: 560px" @submit.prevent>
            <el-form-item label="Name" for="w-name" :error="err('displayName')">
              <el-input id="w-name" v-model="f.name" maxlength="80" />
            </el-form-item>
            <el-form-item label="Slug" for="w-slug" :error="err('slug')">
              <el-input id="w-slug" v-model="f.slug" maxlength="20" @input="f.slugEdited = true" />
            </el-form-item>
            <el-alert
              type="warning"
              :closable="false"
              show-icon
              title="The slug cannot be changed later"
              style="margin-bottom: 18px"
            >
              <div>
                Name in Kubernetes: <code>{{ tenant }}</code>
              </div>
              <div>
                Namespaces: <code>{{ tenant }}-&lt;namespace&gt;</code>
              </div>
              <div v-if="cluster">
                App addresses: <code>&lt;name&gt;-{{ tenant }}.{{ cluster.appsDomain }}</code>
              </div>
            </el-alert>
          </el-form>

          <!-- 2 Cluster and compute -->
          <div v-else-if="w.step.value === 1" class="t-stack" style="gap: 24px">
            <div class="t-stack" style="gap: 10px">
              <h3>Cluster</h3>
              <el-alert
                v-if="clusters.isSuccess.value && !clusters.data.value?.length"
                type="error"
                :closable="false"
                show-icon
                title="No cluster is granted to the Organization"
                description="Ask a Platform Admin for a cluster grant."
              />
              <el-alert
                v-else-if="notReady"
                type="warning"
                :closable="false"
                show-icon
                :title="`${notReady.displayName} is ${notReady.status === 'Maintenance' ? 'in maintenance' : notReady.status.toLowerCase()}`"
                description="Workspaces can be created on it again once it is Ready. Existing Workspaces keep running."
              />
              <div
                v-if="cluster"
                class="t-row t-between"
                style="padding: 12px 14px; border: 1px solid var(--t-line); border-radius: 8px"
              >
                <div class="t-row">
                  <strong>{{ cluster.displayName }}</strong
                  ><StatusTag :value="cluster.status" />
                </div>
                <span v-if="clusters.data.value?.length === 1" class="muted small"
                  >Chosen for you: it is the only cluster granted to {{ orgName }}.</span
                >
                <el-select v-else v-model="f.clusterId" aria-label="Cluster" style="width: 220px">
                  <el-option
                    v-for="c in clusters.data.value"
                    :key="c.id"
                    :value="c.id"
                    :label="c.displayName"
                    :disabled="c.status !== 'Ready'"
                  />
                </el-select>
              </div>
              <div v-if="err('clusterId')" class="t-field-note t-field-err">{{ err('clusterId') }}</div>
              <p class="t-field-note">A Workspace lives on one cluster and cannot be moved to another later.</p>
            </div>

            <div class="t-stack" style="gap: 10px">
              <h3 id="mode-label">Where should the Pods run?</h3>
              <div class="t-options" role="group" aria-labelledby="mode-label">
                <button
                  type="button"
                  class="t-option"
                  :aria-pressed="f.mode === 'Shared'"
                  :disabled="modeOf('Shared')?.available === false"
                  @click="f.mode = 'Shared'"
                >
                  <span class="t-option-title">Shared nodes</span>
                  <span class="muted small"
                    >Nodes shared with other Organizations. Limits can be higher than requests, so Pods may burst when
                    the nodes have room.</span
                  >
                  <span v-if="modeOf('Shared')?.reason" class="t-option-why">{{ modeOf('Shared')?.reason }}</span>
                </button>
                <button
                  type="button"
                  class="t-option"
                  :aria-pressed="f.mode === 'Dedicated'"
                  :disabled="modeOf('Dedicated')?.available === false"
                  @click="f.mode = 'Dedicated'"
                >
                  <span class="t-option-title">Dedicated node</span>
                  <span class="muted small"
                    >A node only {{ orgName }} uses. The CPU and memory you ask for are reserved on it, and limits equal
                    requests.</span
                  >
                  <span v-if="modeOf('Dedicated')?.reason" class="t-option-why">{{ modeOf('Dedicated')?.reason }}</span>
                </button>
              </div>
              <p class="t-field-note">
                Both modes give the same rights inside the Workspace. They only differ in which machines run your Pods.
              </p>
            </div>

            <div v-if="!dedicated" class="t-stack" style="gap: 10px">
              <h3>Shared pool</h3>
              <div class="t-row t-between">
                <el-select
                  v-if="(pools.data.value?.length ?? 0) > 1"
                  v-model="f.sharedPoolId"
                  aria-label="Shared pool"
                  style="width: 260px"
                >
                  <el-option v-for="p in pools.data.value" :key="p.id" :value="p.id" :label="p.name" />
                </el-select>
                <code v-else-if="pool">{{ pool.id }}</code>
                <span v-if="pools.data.value?.length === 1" class="muted small"
                  >Chosen for you: the only pool in your grant.</span
                >
              </div>
              <div v-if="err('compute.sharedPoolId')" class="t-field-note t-field-err">
                {{ err('compute.sharedPoolId') }}
              </div>
              <div v-if="pool" class="t-meters two">
                <QuotaMeter
                  label="Pool CPU reserved"
                  :used="cores(pool.capacity.allocated.cpu)"
                  :total="cores(pool.capacity.ceiling.cpu)"
                  unit="cores"
                />
                <QuotaMeter
                  label="Pool memory reserved"
                  :used="gib(pool.capacity.allocated.memory)"
                  :total="gib(pool.capacity.ceiling.memory)"
                  unit="Gi"
                />
              </div>
            </div>

            <div v-else class="t-stack" style="gap: 10px">
              <h3 id="node-label">Node</h3>
              <p v-if="nodes.isSuccess.value && !nodes.data.value?.length" class="muted">
                {{ orgName }} has no dedicated node on this cluster. A Platform Admin gives nodes.
              </p>
              <div v-else class="t-options" role="group" aria-labelledby="node-label">
                <button
                  v-for="n in nodes.data.value"
                  :key="n.id"
                  type="button"
                  class="t-option"
                  :disabled="!nodeUsable(n)"
                  :aria-pressed="f.nodeGrantId === n.id && nodeUsable(n)"
                  @click="f.nodeGrantId = n.id"
                >
                  <span class="t-option-title"
                    ><code>{{ n.name }}</code
                    ><StatusTag :value="n.nodeState === 'Ready' ? n.capacityState : n.nodeState"
                  /></span>
                  <span class="num"
                    >{{ cores(n.remaining.cpu) }} of {{ cores(n.reservable.cpu) }} cores left ·
                    {{ gib(n.remaining.memory) }} of {{ gib(n.reservable.memory) }} Gi left ·
                    {{ n.remaining.pods }} Pods left</span
                  >
                  <span v-if="n.nodeState !== 'Ready'" class="t-option-why"
                    >The node is {{ n.nodeState === 'NotReady' ? 'not ready' : 'cordoned' }}. Ask a Platform
                    Admin.</span
                  >
                  <span v-else-if="!nodeUsable(n)" class="t-option-why">No room left on this node.</span>
                  <span v-else-if="n.workspaces.length" class="muted small"
                    >Also used by: {{ n.workspaces.join(', ') }}</span
                  >
                </button>
              </div>
              <div v-if="err('compute.dedicatedNodeGrantId')" class="t-field-note t-field-err">
                {{ err('compute.dedicatedNodeGrantId') }}
              </div>
              <p class="t-field-note">
                The node belongs to {{ orgName }}, and several of its Workspaces can share the node's capacity.
              </p>
            </div>
          </div>

          <!-- 3 Resources -->
          <div v-else-if="w.step.value === 2" class="t-stack" style="gap: 20px">
            <p v-if="dedicated" class="t-explain">
              These numbers are reserved on node <code>{{ node?.name }}</code> as soon as the Workspace is created. The
              node belongs to {{ orgName }}, so they are limited by what is left on the node and
              <strong>do not count against the shared quota</strong>. Limits equal requests, which gives Pods Guaranteed
              quality of service.
            </p>
            <p v-else class="t-explain">
              These numbers are taken out of {{ orgName }}'s shared quota on {{ cluster?.displayName }} as soon as the
              Workspace is created, whether or not Pods use them.
            </p>
            <el-form label-position="top" @submit.prevent>
              <div class="t-stack" style="gap: 18px">
                <ResourceGrid v-model="w.form" idp="w" :errors="gridErrors" :lock-limits="dedicated" :notes="notes" />
                <div class="t-fields">
                  <el-form-item label="Pods" for="w-pods" :error="err('resources.pods')">
                    <NumField id="w-pods" v-model="f.pods" :min="1" />
                    <div v-if="left.pods !== undefined" class="t-field-note num">
                      {{ left.pods }} left {{ left.where }}
                    </div>
                  </el-form-item>
                  <el-form-item label="Storage" for="w-sto" :error="err('resources.requestsStorage')">
                    <NumField id="w-sto" v-model="f.storage" unit="Gi" />
                    <div v-if="grant" class="t-field-note num">
                      {{ gib(grant.requestsStorage.remaining) }} left in the grant
                    </div>
                  </el-form-item>
                </div>
              </div>
            </el-form>
            <div class="t-stack" style="gap: 12px">
              <h3>
                {{
                  dedicated ? `Node ${node?.name ?? ''} after this Workspace` : 'The shared quota after this Workspace'
                }}
              </h3>
              <div v-if="dedicated && node" class="t-meters two">
                <QuotaMeter
                  label="CPU reserved"
                  :used="cores(node.reserved.cpu)"
                  :pending="f.rcpu"
                  :total="cores(node.reservable.cpu)"
                  unit="cores"
                />
                <QuotaMeter
                  label="Memory reserved"
                  :used="gib(node.reserved.memory)"
                  :pending="f.rmem"
                  :total="gib(node.reservable.memory)"
                  unit="Gi"
                />
              </div>
              <div v-else-if="!dedicated && grant" class="t-meters tight">
                <QuotaMeter
                  label="CPU requests"
                  :used="cores(grant.requestsCpu.allocated)"
                  :pending="f.rcpu"
                  :total="cores(grant.requestsCpu.limit)"
                  unit="cores"
                />
                <QuotaMeter
                  label="Memory requests"
                  :used="gib(grant.requestsMemory.allocated)"
                  :pending="f.rmem"
                  :total="gib(grant.requestsMemory.limit)"
                  unit="Gi"
                />
                <QuotaMeter
                  label="CPU limits"
                  :used="cores(grant.limitsCpu.allocated)"
                  :pending="f.lcpu"
                  :total="cores(grant.limitsCpu.limit)"
                  unit="cores"
                />
                <QuotaMeter
                  label="Memory limits"
                  :used="gib(grant.limitsMemory.allocated)"
                  :pending="f.lmem"
                  :total="gib(grant.limitsMemory.limit)"
                  unit="Gi"
                />
              </div>
              <p class="t-field-note">Striped part: what this Workspace adds.</p>
            </div>
          </div>

          <!-- 4 Network -->
          <div v-else-if="w.step.value === 3" class="t-stack" style="gap: 18px">
            <el-radio-group v-model="f.netAuto" aria-label="How to pick the Pod network">
              <el-radio :value="true">Pick an address block for me</el-radio>
              <el-radio :value="false">Advanced: choose the block</el-radio>
            </el-radio-group>
            <KeyValues
              v-if="f.netAuto"
              :items="[
                ['Pool', f.networkPoolId],
                ['Block size', `/${prefix}, ${addresses(prefix)} Pod addresses`],
                ['Block', w.block.value?.cidr ?? 'chosen when the Workspace is created'],
              ]"
            >
              <template #v0
                ><code>{{ f.networkPoolId }}</code></template
              >
              <template #v2
                ><span v-if="w.block.value" class="mono">{{ w.block.value.cidr }}</span
                ><span v-if="w.block.value" class="muted"> if created now</span
                ><span v-else>chosen when the Workspace is created</span></template
              >
            </KeyValues>
            <div v-if="f.netAuto && netError" class="t-field-note t-field-err">{{ netError }}</div>
            <el-form v-else-if="!f.netAuto" label-position="top" style="max-width: 560px" @submit.prevent>
              <el-form-item label="Pool" for="w-pool" :error="err('network.networkPoolId')">
                <el-select id="w-pool" v-model="f.networkPoolId" style="width: 100%">
                  <el-option
                    v-for="n in nets.data.value"
                    :key="n.id"
                    :value="n.id"
                    :label="`${n.id}  (${n.cidrs.join(', ')})`"
                  />
                </el-select>
              </el-form-item>
              <el-form-item label="Block size" for="w-prefix" :error="err('network.prefixLength')">
                <el-select id="w-prefix" v-model="f.prefix" style="width: 100%">
                  <el-option
                    :value="0"
                    :label="`The pool's default, /${net?.defaultPrefixLength ?? 24}  (${addresses(net?.defaultPrefixLength ?? 24)} Pod addresses)`"
                  />
                  <el-option
                    v-for="p in net?.allowedPrefixLengths ?? []"
                    :key="p"
                    :value="p"
                    :label="`/${p}  (${addresses(p)} Pod addresses${freeAt(p) !== undefined ? `, ${freeAt(p)} free` : ''})`"
                  />
                </el-select>
              </el-form-item>
              <el-form-item label="Exact block (optional)" for="w-cidr" :error="err('network.requestedCidr')">
                <el-input id="w-cidr" v-model="f.cidr" :placeholder="w.block.value?.cidr ?? '10.240.8.0/24'" />
                <div class="t-field-note">
                  Leave empty to get the next free block of this size. A block that overlaps another Workspace is
                  refused.
                </div>
              </el-form-item>
            </el-form>
            <el-alert
              type="warning"
              :closable="false"
              show-icon
              title="The address block cannot be changed once Pods are running"
              :description="`This Workspace gets ${addresses(prefix)} Pod addresses. Pods of other Workspaces cannot reach them unless you publish a Route.`"
            />
          </div>

          <!-- 5 Namespaces -->
          <div v-else-if="w.step.value === 4" class="t-stack" style="gap: 12px">
            <el-form label-position="top" style="max-width: 560px" @submit.prevent>
              <el-form-item label="Namespace limit" for="w-maxns" :error="err('resources.namespaceQuota')">
                <NumField id="w-maxns" v-model="f.nsQuota" :min="1" :max="100" style="width: 180px" />
                <div class="t-field-note">
                  How many namespaces this Workspace may hold.
                  <template v-if="grant"
                    >{{ orgName }} has {{ grant.namespaces.remaining }} left in its grant.</template
                  >
                </div>
              </el-form-item>
              <el-form-item label="Namespaces to create now (optional)" for="w-ns" :error="err('namespaces')">
                <el-select
                  id="w-ns"
                  v-model="f.namespaces"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  :reserve-keyword="false"
                  placeholder="Type a name and press Enter"
                  style="width: 100%"
                />
                <div class="t-field-note">
                  Created as
                  <span v-for="(n, i) in f.namespaces" :key="n"
                    ><code>{{ tenant }}-{{ n }}</code
                    ><template v-if="i < f.namespaces.length - 1">, </template></span
                  ><span v-if="!f.namespaces.length">nothing yet. You can add namespaces later.</span>
                </div>
              </el-form-item>
            </el-form>
            <p class="t-explain">
              Members with the <strong>TenantOwner</strong> role can create more namespaces later, from the portal or
              with kubectl. Developers and Viewers cannot.
            </p>
          </div>

          <!-- 6 Access -->
          <div v-else-if="w.step.value === 5" class="t-stack">
            <el-form label-position="top" style="max-width: 560px" @submit.prevent>
              <el-form-item
                v-for="r in [
                  {
                    key: 'owners',
                    id: 'w-own',
                    label: 'Owner groups',
                    note: 'TenantOwner: create namespaces, manage workloads and access inside the Workspace.',
                  },
                  {
                    key: 'devs',
                    id: 'w-dev',
                    label: 'Developer groups (optional)',
                    note: 'Developer: deploy and change workloads in existing namespaces.',
                  },
                  { key: 'viewers', id: 'w-view', label: 'Viewer groups (optional)', note: 'Viewer: read only.' },
                ] as const"
                :key="r.key"
                :label="r.label"
                :for="r.id"
                :error="r.key === 'owners' ? err('bindings') : ''"
              >
                <el-select :id="r.id" v-model="f[r.key]" multiple style="width: 100%">
                  <el-option
                    v-for="g in groups.data.value"
                    :key="g.id"
                    :value="g.id"
                    :label="groupLabel(g)"
                    :disabled="groupOff(g, f[r.key])"
                  />
                </el-select>
                <div class="t-field-note">{{ r.note }}</div>
              </el-form-item>
            </el-form>
            <el-alert
              v-if="groups.data.value?.some((g) => g.syncStatus !== 'Synced')"
              type="info"
              :closable="false"
              show-icon
              title="Groups still being synced cannot be chosen yet"
              description="They have not reached the login system. Tokens do not contain them, so their members would get no access."
            />
            <p class="t-explain">
              Nobody in a Workspace can create cluster-wide resources such as nodes, storage classes or custom resource
              definitions.
            </p>
          </div>

          <!-- 7 Review -->
          <div v-else class="t-stack" style="gap: 20px">
            <el-alert
              v-if="w.blocked.value"
              type="error"
              :closable="false"
              show-icon
              title="Fix the steps marked with ! before creating the Workspace"
            />
            <KeyValues
              :items="[
                ['Name', f.name],
                ['Cluster', `${cluster?.displayName ?? f.clusterId} (cannot be changed later)`],
                ['Compute', dedicated ? `Dedicated node ${node?.name ?? ''}` : `Shared pool ${f.sharedPoolId}`],
                ['CPU', `${plural(f.rcpu, 'core')} requested, ${plural(f.lcpu, 'core')} limit`],
                ['Memory', `${f.rmem} Gi requested, ${f.lmem} Gi limit`],
                ['Pods and storage', `${f.pods} Pods, ${f.storage} Gi`],
                [
                  'Pod network',
                  (w.block.value?.cidr ?? (f.netAuto || !f.cidr ? `A free /${prefix}` : f.cidr)) +
                    ` from ${f.networkPoolId}`,
                ],
                [
                  'Namespaces',
                  `Up to ${f.nsQuota}` + (f.namespaces.length ? `; creating ${f.namespaces.join(', ')}` : ''),
                ],
                ['Owners', f.owners.map(groupName).join(', ') || 'none'],
                ['Developers', f.devs.map(groupName).join(', ') || 'none'],
                ['Viewers', f.viewers.map(groupName).join(', ') || 'none'],
              ]"
            >
              <template #v0
                >{{ f.name }} <code>{{ tenant }}</code></template
              >
            </KeyValues>
            <p class="t-explain">
              Creating takes about a minute. You can follow each step on the next page and leave it at any time.
            </p>
          </div>
        </div>

        <div class="t-wfoot">
          <el-button :disabled="w.step.value === 0" @click="move(w.back)">Back</el-button>
          <div class="t-row">
            <el-button @click="router.push(`/orgs/${orgSlug}/workspaces`)">Cancel</el-button>
            <el-button
              v-if="w.step.value < steps.length - 1"
              type="primary"
              :loading="w.checking.value"
              @click="move(w.next)"
              >Continue</el-button
            >
            <el-button
              v-else
              type="primary"
              :disabled="w.blocked.value"
              :loading="create.isPending.value"
              @click="submit"
              >Create Workspace</el-button
            >
          </div>
        </div>
      </section>
    </div>
  </AppShell>
</template>
