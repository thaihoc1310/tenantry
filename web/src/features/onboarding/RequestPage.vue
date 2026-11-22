<script setup lang="ts">
// Before an Organization (docs/design/mockups: Welcome): request one, wait
// for the review, or read why it was rejected. "/" sends members of an
// Active Organization to its Overview; "/request" is for asking another.
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { when } from '@/app/format'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { showFailure } from '@/app/failure'
import { errorText } from '@/i18n/errors'
import { useCreateOrganization, useMe, useOrganizations } from '@/features/organizations/queries'

const props = defineProps<{ another?: boolean }>()
const router = useRouter()
const me = useMe()
const orgs = useOrganizations()
const create = useCreateOrganization()

const appsDomain = import.meta.env.VITE_APPS_DOMAIN || 'mvp.tenantry.site'

const list = computed(() => orgs.data.value ?? [])
const firstActive = computed(() => list.value.find((o) => o.status === 'Active'))
const pending = computed(() => list.value.find((o) => o.status === 'Pending'))
// The newest item is the latest request; show its rejection until a new one is filed.
const rejected = computed(() => (list.value[0]?.status === 'Rejected' ? list.value[0] : undefined))
const writing = ref(false)

watch(
  firstActive,
  (o) => {
    if (o && !props.another) router.replace(`/orgs/${o.slug}`)
  },
  { immediate: true },
)

const state = computed(() => {
  if (pending.value) return 'pending'
  if (rejected.value && !writing.value && !props.another) return 'rejected'
  return 'form'
})

// The form. The slug follows the display name until it is edited.
const form = reactive({ name: '', slug: '', purpose: '' })
const slugEdited = ref(false)
const fieldErrors = reactive<{ slug: string; displayName: string; description: string }>({
  slug: '',
  displayName: '',
  description: '',
})
const failure = ref<ApiError>()

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

// No hyphen: <org>-<workspace> must name one Workspace of one organization (PRODUCT-PLAN §4.2).
const slugRule = computed(() => {
  if (!form.slug) return ''
  if (form.slug.length < 3 || form.slug.length > 20) return 'Use 3 to 20 characters.'
  if (!/^[a-z][a-z0-9]*$/.test(form.slug)) return 'Use lowercase letters and digits only, starting with a letter.'
  return ''
})
const slugError = computed(() => fieldErrors.slug || slugRule.value)
const shown = computed(() => form.slug || 'yourslug')
const canSend = computed(
  () => form.name.trim() !== '' && form.slug !== '' && !slugRule.value && !create.isPending.value,
)

async function send() {
  Object.assign(fieldErrors, { slug: '', displayName: '', description: '' })
  failure.value = undefined
  try {
    await create.mutateAsync({
      slug: form.slug,
      displayName: form.name.trim(),
      description: form.purpose.trim() || undefined,
    })
    writing.value = false
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = err
    const text = `${errorText[err.code].en} (${err.code})`
    if (err.code === 'SLUG_TAKEN') fieldErrors.slug = text
    else if (err.field in fieldErrors)
      fieldErrors[err.field as keyof typeof fieldErrors] = `${err.message} (${err.code})`
    else if (err.code === 'ORG_REQUEST_PENDING') orgs.refetch()
    else showFailure(err)
  }
}

async function checkAgain() {
  const { data } = await orgs.refetch()
  const approved = data?.find((o) => o.id === pending.value?.id && o.status === 'Active')
  if (approved) router.push(`/orgs/${approved.slug}`)
}
</script>

<template>
  <AppShell area="bare" narrow>
    <ErrorState v-if="orgs.isError.value" :error="orgs.error.value" @retry="orgs.refetch()" />
    <el-skeleton v-else-if="orgs.isPending.value" :rows="6" animated />

    <template v-else-if="state === 'form'">
      <PageHeader
        title="Request an Organization"
        description="An Organization owns your Workspaces, members and quota. A Platform Admin reviews each request and sets the starting quota."
      />
      <TPanel>
        <el-form label-position="top" @submit.prevent="send">
          <el-form-item label="Display name" for="org-name" :error="fieldErrors.displayName">
            <el-input id="org-name" v-model="form.name" maxlength="80" />
          </el-form-item>
          <el-form-item label="Slug" for="org-slug" :error="slugError">
            <el-input id="org-slug" v-model="form.slug" maxlength="20" @input="slugEdited = true" />
            <div v-if="!slugError" class="t-field-note">3 to 20 lowercase letters or digits, no hyphen.</div>
          </el-form-item>
          <el-alert
            type="warning"
            :closable="false"
            show-icon
            title="The slug cannot be changed later"
            style="margin-bottom: 18px"
          >
            <div>It becomes part of every name Kubernetes sees:</div>
            <div>
              Workspaces: <code>{{ shown }}-&lt;workspace&gt;</code>
            </div>
            <div>
              App addresses: <code>&lt;name&gt;-{{ shown }}-&lt;workspace&gt;.{{ appsDomain }}</code>
            </div>
          </el-alert>
          <el-form-item label="What will you use it for?" for="org-purpose" :error="fieldErrors.description">
            <el-input
              id="org-purpose"
              v-model="form.purpose"
              type="textarea"
              :rows="3"
              maxlength="500"
              show-word-limit
            />
            <div class="t-field-note">
              The Platform Admin decides your quota from this. Say how many environments you need and roughly how big
              they are.
            </div>
          </el-form-item>
          <div class="t-row t-between">
            <span v-if="failure" class="t-errcode">{{ failure.code }} · request {{ failure.requestId }}</span
            ><span v-else></span>
            <el-button type="primary" native-type="submit" :disabled="!canSend" :loading="create.isPending.value"
              >Send request</el-button
            >
          </div>
        </el-form>
      </TPanel>
      <TPanel title="Joining an existing Organization?">
        <p class="muted">
          Ask its owner for an invitation link, then open the link while logged in as
          <strong>{{ me.data.value?.email ?? me.data.value?.displayName }}</strong
          >.
        </p>
      </TPanel>
    </template>

    <template v-else-if="state === 'pending' && pending">
      <PageHeader title="Your request is waiting for review">
        <template #tags><StatusTag value="Pending" /></template>
      </PageHeader>
      <TPanel title="What you asked for">
        <KeyValues
          :items="[
            ['Display name', pending.displayName],
            ['Slug', pending.slug],
            ['Purpose', pending.description || '—'],
            ['Sent', when(pending.createdAt)],
          ]"
        >
          <template #v1
            ><code>{{ pending.slug }}</code>
            <span class="muted small">held for you while the request is open</span></template
          >
        </KeyValues>
      </TPanel>
      <TPanel title="What happens next">
        <div class="t-stack">
          <p>
            A Platform Admin reads the request and decides the starting quota. When it is approved, this page becomes
            the Overview of your Organization. If it is rejected, the reason appears here and you can send a new
            request.
          </p>
          <div class="t-row t-between">
            <span class="muted small">You can have one open request at a time.</span>
            <el-button :loading="orgs.isFetching.value" @click="checkAgain"
              ><el-icon><Refresh /></el-icon>&nbsp;Check again</el-button
            >
          </div>
        </div>
      </TPanel>
    </template>

    <template v-else-if="rejected">
      <PageHeader title="Your request was not approved">
        <template #tags><StatusTag value="Rejected" /></template>
      </PageHeader>
      <TPanel title="Reason from the Platform Admin">
        <div class="t-stack">
          <el-alert type="error" :closable="false" :title="rejected.reviewNote" />
          <KeyValues
            :items="[
              ['Display name', rejected.displayName],
              ['Slug', rejected.slug],
              ['Sent', when(rejected.createdAt)],
            ]"
          >
            <template #v1
              ><code>{{ rejected.slug }}</code>
              <span class="muted small">released, you can request it again</span></template
            >
          </KeyValues>
        </div>
      </TPanel>
      <div class="t-row" style="justify-content: flex-end">
        <el-button type="primary" @click="writing = true">Send a new request</el-button>
      </div>
    </template>
  </AppShell>
</template>
