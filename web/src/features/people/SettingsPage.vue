<script setup lang="ts">
// An Organization's settings (docs/design/mockups: Settings and Transfer):
// its details, handing it to an OrgAdmin (the waiting offer shows to the owner
// and to its recipient), and leaving it. Deleting an Organization comes with
// Workspaces (M6).
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { expiresIn, when } from '@/app/format'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import TPanel from '@/components/TPanel.vue'
import { useMe, useOrganization } from '@/features/organizations/queries'
import {
  useAcceptTransfer,
  useCancelTransfer,
  useLeave,
  useMembers,
  usePatchOrganization,
  useProposeTransfer,
  useTransfer,
} from './queries'

const props = defineProps<{ orgSlug: string }>()
const slug = () => props.orgSlug
const router = useRouter()

const me = useMe()
const org = useOrganization(slug)
const role = computed(() => org.data.value?.myRole)
const owner = computed(() => role.value === 'OrgOwner')
const manages = computed(() => owner.value || role.value === 'OrgAdmin')
const active = computed(() => org.data.value?.status === 'Active')

const members = useMembers(slug, () => owner.value && active.value)
const transfer = useTransfer(slug, () => manages.value && active.value)
const patch = usePatchOrganization(slug)
const propose = useProposeTransfer(slug)
const accept = useAcceptTransfer(slug)
const cancel = useCancelTransfer(slug)
const leave = useLeave(slug)

const t = computed(() => transfer.data.value ?? null)
const forMe = computed(() => !!t.value && t.value.to.id === me.data.value?.id)
const admins = computed(() => (members.data.value ?? []).filter((m) => m.role === 'OrgAdmin'))
const person = (u: { displayName: string; email?: string }) =>
  u.email ? `${u.displayName} (${u.email})` : u.displayName

// ---- details --------------------------------------------------------------
const details = reactive({ displayName: '', description: '' })
const nameError = ref('')
watch(
  () => org.data.value,
  (o) => {
    if (o) Object.assign(details, { displayName: o.displayName, description: o.description })
  },
  { immediate: true },
)
const changed = computed(
  () =>
    !!org.data.value &&
    (details.displayName.trim() !== org.data.value.displayName ||
      details.description.trim() !== org.data.value.description),
)
async function save() {
  nameError.value = ''
  try {
    await patch.mutateAsync({ displayName: details.displayName.trim(), description: details.description.trim() })
    ElMessage.success('Saved.')
  } catch (err) {
    if (err instanceof ApiError && err.field === 'displayName') nameError.value = err.message
    else showFailure(err)
  }
}

// ---- transfer -------------------------------------------------------------
const to = ref('')
const understood = ref(false)
watch(t, () => (understood.value = false))

async function startTransfer() {
  try {
    const r = await propose.mutateAsync(to.value)
    to.value = ''
    ElMessage.success(`${r.to.displayName} can accept until ${when(r.expiresAt)}.`)
  } catch (err) {
    showFailure(err)
  }
}

async function acceptTransfer() {
  if (!t.value) return
  try {
    await accept.mutateAsync(t.value.id)
    ElMessage.success(`You are now the owner of ${org.data.value?.displayName}.`)
  } catch (err) {
    showFailure(err)
  }
}

async function cancelTransfer(declining: boolean) {
  if (!t.value) return
  try {
    await cancel.mutateAsync(t.value.id)
    ElMessage.success(declining ? 'You declined; nothing changed.' : 'The offer was withdrawn.')
  } catch (err) {
    showFailure(err)
  }
}

// ---- leave ----------------------------------------------------------------
async function leaveOrg() {
  const name = org.data.value?.displayName ?? 'this Organization'
  try {
    await ElMessageBox.confirm(
      `You lose access to ${name} and its Workspaces in the portal now, and in Kubernetes within 5 minutes. Coming back takes a new invitation.`,
      `Leave ${name}?`,
      { type: 'warning', confirmButtonText: `Leave ${name}`, confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  try {
    await leave.mutateAsync()
    ElMessage.success(`You left ${name}.`)
    router.replace('/')
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell active="settings" :org-slug="orgSlug">
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="!org.data.value" :rows="8" animated />

    <template v-else>
      <PageHeader title="Settings" />

      <!-- The recipient's side of a waiting transfer (mockups: Transfer), first: it waits for them. -->
      <TPanel v-if="t && forMe" :title="`Become the owner of ${org.data.value.displayName}`">
        <div class="t-stack" style="max-width: 640px">
          <KeyValues
            :items="[
              ['Organization', org.data.value.displayName],
              ['Current owner', person(t.from)],
              ['You', person(t.to) + ', OrgAdmin'],
              ['Request expires', expiresIn(t.expiresAt)],
            ]"
          />
          <el-alert type="warning" :closable="false" show-icon title="What changes when you accept">
            <div>You become <strong>OrgOwner</strong>: only you can hand the Organization over again.</div>
            <div>
              {{ t.from.displayName }} becomes <strong>OrgAdmin</strong>. Workspaces, groups and quota stay as they are.
            </div>
          </el-alert>
          <el-checkbox id="transfer-ok" v-model="understood"
            >I understand that I become responsible for this Organization.</el-checkbox
          >
          <div class="t-row" style="justify-content: flex-end">
            <el-button :loading="cancel.isPending.value" @click="cancelTransfer(true)">Decline</el-button>
            <el-button type="primary" :disabled="!understood" :loading="accept.isPending.value" @click="acceptTransfer"
              >Accept ownership</el-button
            >
          </div>
        </div>
      </TPanel>

      <TPanel v-if="manages" title="Details">
        <el-form label-position="top" style="max-width: 520px" @submit.prevent="save">
          <el-form-item label="Display name" for="set-name" :error="nameError">
            <el-input id="set-name" v-model="details.displayName" maxlength="80" />
          </el-form-item>
          <el-form-item label="Slug" for="set-slug">
            <el-input id="set-slug" :model-value="org.data.value.slug" disabled />
            <div class="t-field-note">The slug is part of every Kubernetes name and cannot be changed.</div>
          </el-form-item>
          <el-form-item label="Description" for="set-desc">
            <el-input id="set-desc" v-model="details.description" type="textarea" :rows="2" maxlength="500" />
          </el-form-item>
          <el-button
            type="primary"
            :disabled="!changed || !details.displayName.trim()"
            :loading="patch.isPending.value"
            @click="save"
            >Save changes</el-button
          >
        </el-form>
      </TPanel>

      <TPanel v-if="owner && active" title="Transfer ownership">
        <div class="t-stack" style="max-width: 560px">
          <template v-if="t">
            <el-alert
              type="info"
              :closable="false"
              show-icon
              :title="`Waiting for ${t.to.displayName} to accept`"
              :description="`Offered ${when(t.createdAt)}; the offer expires ${expiresIn(t.expiresAt)}. Until they accept you stay the owner.`"
            />
            <div>
              <el-button :loading="cancel.isPending.value" @click="cancelTransfer(false)">Withdraw the offer</el-button>
            </div>
          </template>
          <template v-else>
            <p class="muted">
              Hand this Organization to one of its OrgAdmins. They must accept within 7 days; until then you stay the
              owner. Afterwards you become OrgAdmin.
            </p>
            <el-form label-position="top" @submit.prevent="startTransfer">
              <el-form-item label="New owner" for="set-owner">
                <el-select
                  id="set-owner"
                  v-model="to"
                  style="width: 100%"
                  placeholder="Choose an OrgAdmin"
                  :loading="members.isPending.value"
                  no-data-text="No OrgAdmin yet"
                >
                  <el-option v-for="m in admins" :key="m.userId" :value="m.userId" :label="person(m) + ', OrgAdmin'" />
                </el-select>
                <div class="t-field-note">
                  Only OrgAdmins are listed. Change a member's role on the Members page first.
                </div>
              </el-form-item>
              <el-button :disabled="!to" :loading="propose.isPending.value" @click="startTransfer"
                >Start transfer</el-button
              >
            </el-form>
          </template>
        </div>
      </TPanel>

      <TPanel v-if="!owner" title="Leave this Organization">
        <div class="t-stack" style="max-width: 560px">
          <p class="muted">
            You lose access to {{ org.data.value.displayName }} and its Workspaces in the portal now, and in Kubernetes
            within 5 minutes.
          </p>
          <div>
            <el-button type="danger" plain :loading="leave.isPending.value" @click="leaveOrg"
              >Leave {{ org.data.value.displayName }}</el-button
            >
          </div>
        </div>
      </TPanel>
      <TPanel v-else title="Leave this Organization">
        <p class="muted">The owner cannot leave. Transfer ownership first; then you can leave as an OrgAdmin.</p>
      </TPanel>
    </template>
  </AppShell>
</template>
