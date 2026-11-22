<script setup lang="ts">
// Create an Organization for a customer (docs/design/mockups: AdminOrgs,
// dialog): active at once, with its owner and starting grant. The owner must
// have logged in once (422 OWNER_USER_NOT_FOUND, PRODUCT-PLAN §16.5).
import { ElMessage } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { showFailure } from '@/app/failure'
import { errorText } from '@/i18n/errors'
import GrantForm from './GrantForm.vue'
import { emptyGrant, fieldOf, fromApproval, type GrantModel, toApproval } from './grant'
import { useCreateOrganization, usePolicy, useUsers } from './queries'

const open = defineModel<boolean>({ required: true })
const router = useRouter()

const policy = usePolicy()
const create = useCreateOrganization()
const form = reactive({ name: '', slug: '', owner: '' })
const slugEdited = ref(false)
const ownerQuery = ref('')
const users = useUsers(ownerQuery)
const grant = ref<GrantModel>(emptyGrant())
const errors = ref<Partial<Record<keyof GrantModel | 'name' | 'slug' | 'owner', string>>>({})
const refused = ref<ApiError>()

const options = computed(() => {
  const g = policy.data.value?.clusterGrant
  return { pools: g?.sharedPoolIds ?? [], nets: g?.networkPoolIds ?? [], classes: g?.allowedStorageClasses ?? [] }
})
const owners = computed(() => (users.data.value?.items ?? []).filter((u) => u.status === 'Active' && u.email))

// Every opening is a new form, filled in from the onboarding policy.
watch(open, (o) => {
  if (!o) return
  Object.assign(form, { name: '', slug: '', owner: '' })
  slugEdited.value = false
  errors.value = {}
  refused.value = undefined
  grant.value = policy.data.value ? fromApproval(policy.data.value) : emptyGrant()
  create.newKey()
})
watch(
  () => policy.data.value,
  (p) => {
    if (p && open.value) grant.value = fromApproval(p)
  },
)
watch(
  () => form.name,
  (v) => {
    if (!slugEdited.value)
      form.slug = v
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '')
        .replace(/^[^a-z]+/, '')
        .slice(0, 20)
  },
)

async function submit() {
  errors.value = {}
  refused.value = undefined
  const a = toApproval(grant.value)
  try {
    const out = await create.mutateAsync({
      slug: form.slug,
      displayName: form.name.trim(),
      ownerEmail: form.owner,
      entitlement: a.entitlement,
      clusterGrant: a.clusterGrant,
    })
    open.value = false
    ElMessage.success(`${out.organization.displayName} is active, owned by ${form.owner}.`)
    router.push(`/admin/organizations/${out.organization.id}`)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    const field = { slug: 'slug', displayName: 'name', ownerEmail: 'owner' }[err.field] as
      'slug' | 'name' | 'owner' | undefined
    if (err.code === 'SLUG_TAKEN') errors.value = { slug: err.message }
    else if (err.code === 'OWNER_USER_NOT_FOUND') errors.value = { owner: errorText.OWNER_USER_NOT_FOUND.en }
    else if (field) errors.value = { [field]: err.message }
    else if (fieldOf[err.field]) errors.value = { [fieldOf[err.field]!]: err.message }
    else if (err.code === 'CLUSTER_NOT_READY') refused.value = err
    else showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" title="Create Organization" width="900px">
    <p class="muted" style="margin-bottom: 14px">
      For a customer you set up yourself. The Organization is active right away, with no request to review.
    </p>
    <el-form label-position="top" @submit.prevent="submit">
      <div class="t-grid-form">
        <el-form-item label="Display name" for="co-name" :error="errors.name">
          <el-input id="co-name" v-model="form.name" maxlength="80" />
        </el-form-item>
        <el-form-item label="Slug" for="co-slug" :error="errors.slug">
          <el-input id="co-slug" v-model="form.slug" maxlength="20" @input="slugEdited = true" />
        </el-form-item>
        <el-form-item label="Owner" for="co-owner" :error="errors.owner">
          <el-select
            id="co-owner"
            v-model="form.owner"
            filterable
            remote
            :remote-method="(q: string) => (ownerQuery = q)"
            :loading="users.isFetching.value"
            placeholder="Search by email"
            style="width: 100%"
          >
            <el-option v-for="u in owners" :key="u.id" :value="u.email!" :label="u.email!" />
          </el-select>
          <div class="t-field-note">Only people who have logged in at least once are listed.</div>
        </el-form-item>
      </div>
    </el-form>
    <el-alert
      v-if="refused"
      type="error"
      :closable="false"
      show-icon
      :title="errorText[refused.code].en"
      :description="refused.message"
      style="margin-bottom: 12px"
    />
    <el-alert
      v-if="policy.isSuccess.value && !policy.data.value"
      type="warning"
      :closable="false"
      show-icon
      title="No onboarding policy is active"
      description="Fill in the grant yourself: the cluster, a network pool and the quota."
      style="margin-bottom: 12px"
    />
    <el-form-item
      v-if="policy.isSuccess.value && !policy.data.value"
      label="Cluster"
      for="co-cluster"
      style="max-width: 300px"
    >
      <el-input id="co-cluster" v-model="grant.clusterId" placeholder="cluster-a" />
    </el-form-item>
    <h3 style="margin: 4px 0 12px">Starting grant on {{ grant.clusterId || 'a cluster' }}</h3>
    <GrantForm v-model="grant" :errors="errors" :options="options" idp="co" />
    <template #footer>
      <el-button @click="open = false">Cancel</el-button>
      <el-button
        type="primary"
        :loading="create.isPending.value"
        :disabled="!form.name.trim() || !form.slug || !form.owner"
        @click="submit"
        >Create Organization</el-button
      >
    </template>
  </el-dialog>
</template>
