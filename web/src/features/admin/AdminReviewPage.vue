<script setup lang="ts">
// Review one request (docs/design/mockups: AdminReview): approve it with a
// grant filled in from the onboarding policy, or reject it with a reason.
import { ElMessage, ElNotification } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { when } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { errorText } from '@/i18n/errors'
import GrantForm from './GrantForm.vue'
import { emptyGrant, fieldOf, fromApproval, type GrantModel, toApproval } from './grant'
import { useAdminOrganizations, useApprove, useReject } from './queries'

const props = defineProps<{ orgId: string }>()
const router = useRouter()

// There is no "get one" admin endpoint: the request is found in the queue.
const queue = useAdminOrganizations('Pending', 200)
const req = computed(() => queue.items.value.find((o) => o.id === props.orgId))

const grant = ref<GrantModel>(emptyGrant())
const options = ref({ pools: [] as string[], nets: [] as string[], classes: [] as string[] })
watch(
  req,
  (r, before) => {
    if (!r || before) return // fill the form once; later refetches keep the Admin's edits
    if (r.proposal) {
      grant.value = fromApproval(r.proposal)
      const g = r.proposal.clusterGrant
      options.value = { pools: g.sharedPoolIds, nets: g.networkPoolIds, classes: g.allowedStorageClasses }
    }
  },
  { immediate: true },
)

const approve = useApprove()
const reject = useReject()
const errors = ref<Partial<Record<keyof GrantModel, string>>>({})
const failure = ref<ApiError>()
const rejecting = ref(false)
const reason = ref('')

function explain(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  failure.value = err
  if (err.code === 'CONFLICT') {
    ElMessage.warning('Another Platform Admin reviewed this request in the meantime.')
    router.push('/admin/organizations')
    return
  }
  const field = fieldOf[err.field]
  if (field) errors.value = { [field]: `${err.message} (${err.code})` }
  else if (err.code !== 'CLUSTER_NOT_READY')
    ElNotification.error({ title: errorText[err.code].en, message: `${err.code} · request ${err.requestId}` })
}

async function doApprove() {
  errors.value = {}
  failure.value = undefined
  try {
    await approve.mutateAsync({ orgId: props.orgId, approval: toApproval(grant.value) })
    ElMessage.success(`${req.value?.displayName ?? 'The Organization'} is active.`)
    router.push({ path: '/admin/organizations', query: { tab: 'active' } })
  } catch (err) {
    explain(err)
  }
}

async function doReject() {
  failure.value = undefined
  try {
    await reject.mutateAsync({ orgId: props.orgId, reason: reason.value.trim() })
    ElMessage.success('The request was rejected; the requester sees your reason.')
    router.push({ path: '/admin/organizations', query: { tab: 'rejected' } })
  } catch (err) {
    explain(err)
  }
}

const by = computed(() => req.value?.owner.email ?? req.value?.owner.displayName ?? '')
</script>

<template>
  <AppShell area="admin" active="organizations">
    <ErrorState v-if="queue.isError.value" :error="queue.error.value" @retry="queue.refetch()" />
    <el-skeleton v-else-if="queue.isPending.value" :rows="8" animated />
    <TPanel v-else-if="!req">
      <EmptyState
        title="This request is not waiting for review"
        description="It was approved or rejected already, or the link is wrong."
      >
        <template #actions
          ><el-button type="primary" @click="router.push('/admin/organizations')"
            >Back to Organizations</el-button
          ></template
        >
      </EmptyState>
    </TPanel>

    <template v-else>
      <PageHeader
        :title="req.displayName"
        :crumbs="[{ label: 'Organizations', to: '/admin/organizations' }, { label: 'Review request' }]"
      >
        <template #tags
          ><code>{{ req.slug }}</code
          ><StatusTag value="Pending" label="Waiting for review"
        /></template>
      </PageHeader>

      <TPanel title="Request">
        <KeyValues
          :items="[
            ['Requested by', by],
            ['Purpose', req.description || 'Not given'],
            ['Waiting since', when(req.createdAt)],
          ]"
        >
          <template #v0
            >{{ by }} <StatusTag tone="ok" label="Email verified" />
            <span class="muted small">becomes OrgOwner</span></template
          >
        </KeyValues>
      </TPanel>

      <TPanel
        :title="`Starting grant on ${grant.clusterId || 'a cluster'}`"
        :note="req.proposal ? 'filled in from the onboarding policy' : undefined"
      >
        <div class="t-stack">
          <el-alert
            v-if="!req.proposal"
            type="warning"
            :closable="false"
            show-icon
            title="No onboarding policy is active"
            description="Fill in the grant yourself: the cluster, a network pool and the quota."
          />
          <el-form-item v-if="!req.proposal" label="Cluster" for="rv-cluster" style="max-width: 300px">
            <el-input id="rv-cluster" v-model="grant.clusterId" placeholder="cluster-a" />
          </el-form-item>
          <el-alert
            v-if="failure?.code === 'CLUSTER_NOT_READY'"
            type="error"
            :closable="false"
            show-icon
            :title="errorText.CLUSTER_NOT_READY.en"
            :description="`Choose another cluster or wait until ${grant.clusterId} is Ready. (CLUSTER_NOT_READY · request ${failure.requestId})`"
          />
          <GrantForm v-model="grant" :errors="errors" :options="options" idp="rv" />
          <p class="t-explain">
            Approving creates the Organization, this grant and the groups <code>admins</code>,
            <code>developers</code> and <code>viewers</code>, with {{ by }} in <code>admins</code>. Later changes to the
            onboarding policy do not change this grant.
          </p>
          <div class="t-row t-between">
            <el-button type="danger" plain @click="rejecting = !rejecting">Reject…</el-button>
            <el-button type="primary" :loading="approve.isPending.value" @click="doApprove"
              >Approve with this grant</el-button
            >
          </div>
        </div>
      </TPanel>

      <TPanel v-if="rejecting" title="Reject this request" danger>
        <el-form label-position="top" style="max-width: 640px" @submit.prevent="doReject">
          <el-form-item label="Reason, shown to the requester" for="rv-reason">
            <el-input id="rv-reason" v-model="reason" type="textarea" :rows="3" maxlength="500" show-word-limit />
            <div class="t-field-note">
              Say what they should change. Rejecting releases the slug <code>{{ req.slug }}</code
              >, and they can send a new request.
            </div>
          </el-form-item>
          <el-button
            type="danger"
            native-type="submit"
            :disabled="reason.trim().length < 10"
            :loading="reject.isPending.value"
            >Reject request</el-button
          >
          <span v-if="reason.trim().length < 10" class="t-field-note" style="margin-left: 10px"
            >Write a reason first.</span
          >
        </el-form>
      </TPanel>
    </template>
  </AppShell>
</template>
