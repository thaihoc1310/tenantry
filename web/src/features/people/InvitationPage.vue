<script setup lang="ts">
// The invitation link (docs/design/mockups: Invitation): who invites you
// where and as what, then accept. Only the verified email the invitation was
// sent to can accept it (PRODUCT-PLAN §9.1). A visitor who is not logged in is
// sent to Keycloak by the first call and comes back here.
import { ElMessage } from 'element-plus'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, login } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { expiresIn } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { errorText } from '@/i18n/errors'
import { logout, useMe } from '@/features/organizations/queries'
import { useAcceptInvitation, useInvitationPreview } from './queries'

const props = defineProps<{ token: string }>()
const router = useRouter()

const me = useMe()
const preview = useInvitationPreview(() => props.token)
const accept = useAcceptInvitation()
const p = computed(() => preview.data.value)

// A link that is unknown, used, revoked or expired: all look the same.
const gone = ref(false)
const invalid = computed(
  () => gone.value || (preview.error.value instanceof ApiError && preview.error.value.code === 'INVITATION_INVALID'),
)
const refused = ref<ApiError>()

async function doAccept() {
  refused.value = undefined
  try {
    const org = await accept.mutateAsync(props.token)
    ElMessage.success(`Welcome to ${org.displayName}.`)
    router.replace(`/orgs/${org.slug}`)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.code === 'INVITATION_INVALID') gone.value = true
    else if (err.code === 'CONFLICT' && p.value)
      router.replace(`/orgs/${p.value.orgSlug}`) // a member already
    else if (err.code === 'ENTITLEMENT_EXCEEDED' || err.code === 'INVITATION_EMAIL_MISMATCH') refused.value = err
    else showFailure(err)
  }
}

async function switchAccount() {
  try {
    await logout()
  } catch {
    ElMessage.error('The platform could not be reached; you are still logged in. Try again.')
    return
  }
  login(`/invitations/${props.token}`)
}
</script>

<template>
  <AppShell area="bare" narrow>
    <template v-if="invalid">
      <PageHeader title="This invitation link no longer works" />
      <TPanel>
        <EmptyState
          title="The link expired or was already used"
          description="Invitation links work once and expire after 7 days. Ask the person who invited you to send a new one."
        >
          <template #actions><el-button @click="router.replace('/')">Go to the portal</el-button></template>
        </EmptyState>
      </TPanel>
    </template>

    <ErrorState v-else-if="preview.isError.value" :error="preview.error.value" @retry="preview.refetch()" />
    <el-skeleton v-else-if="!p" :rows="6" animated />

    <template v-else-if="p.alreadyMember">
      <PageHeader title="Nothing to accept" />
      <TPanel>
        <EmptyState
          :title="`You are already in ${p.orgName}`"
          description="This invitation is for an Organization you are a member of; there is nothing to accept."
        >
          <template #actions
            ><el-button type="primary" @click="router.replace(`/orgs/${p.orgSlug}`)"
              >Open {{ p.orgName }}</el-button
            ></template
          >
        </EmptyState>
      </TPanel>
    </template>

    <template v-else-if="!p.emailMatches">
      <PageHeader title="This invitation is for another account" />
      <TPanel>
        <div class="t-stack">
          <el-alert
            type="error"
            :closable="false"
            show-icon
            :title="`The invitation was sent to ${p.email}, but you are logged in as ${me.data.value?.email ?? 'another account'}.`"
            description="Log out and log in again with the invited email address. Nothing has been changed."
          />
          <p class="t-errcode">INVITATION_EMAIL_MISMATCH</p>
          <div class="t-row" style="justify-content: flex-end">
            <el-button type="primary" @click="switchAccount">Log out and switch account</el-button>
          </div>
        </div>
      </TPanel>
    </template>

    <template v-else>
      <PageHeader
        :title="`Join ${p.orgName}`"
        :description="`${p.invitedBy} invited you to the Organization ${p.orgName}.`"
      />
      <TPanel>
        <div class="t-stack">
          <el-alert
            v-if="refused"
            type="error"
            :closable="false"
            show-icon
            :title="errorText[refused.code].en"
            :description="refused.message"
          />
          <KeyValues
            :items="[
              ['Invited email', p.email],
              ['You are logged in as', me.data.value?.email ?? ''],
              ['Your role', p.role],
              ['Groups', p.groupNames.join(', ') || 'None'],
              ['Link expires', expiresIn(p.expiresAt)],
            ]"
          >
            <template #v1>{{ me.data.value?.email }} <StatusTag tone="ok" label="Verified, matches" /></template>
          </KeyValues>
          <p v-if="p.role === 'OrgAdmin'" class="t-explain">
            As <strong>OrgAdmin</strong> you manage the members, groups and Workspaces of {{ p.orgName }}.
          </p>
          <p v-else class="t-explain">
            As <strong>OrgMember</strong> you see the Organization and the Workspaces your groups can reach.
            <template v-if="p.groupNames.length">Your groups decide what you can do inside those Workspaces.</template>
          </p>
          <div class="t-row" style="justify-content: flex-end">
            <el-button @click="router.replace('/')">Not now</el-button>
            <el-button type="primary" :loading="accept.isPending.value" @click="doAccept">Accept invitation</el-button>
          </div>
        </div>
      </TPanel>
    </template>
  </AppShell>
</template>
