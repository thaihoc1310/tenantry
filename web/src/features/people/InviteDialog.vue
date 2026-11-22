<script setup lang="ts">
// Invite a member (docs/design/mockups: Members, dialog): an email, a role,
// groups; then the one-time link, shown once (PRODUCT-PLAN §9.1).
import { computed, reactive, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import { showFailure } from '@/app/failure'
import CopyField from '@/components/CopyField.vue'
import { errorText, refusalDetail } from '@/i18n/errors'
import { type Group, type Invitation, type InvitationRole, useInvite } from './queries'

const props = defineProps<{ orgSlug: string; groups: Group[] }>()
const open = defineModel<boolean>({ required: true })

const invite = useInvite(() => props.orgSlug)
const form = reactive({ email: '', role: 'OrgMember' as InvitationRole, groupIds: [] as string[] })
const created = ref<Invitation>()
const emailError = ref('')
const refused = ref<ApiError>()

// Every opening is a new form: blank fields, a new idempotency key.
watch(open, (o) => {
  if (!o) return
  Object.assign(form, { email: '', role: 'OrgMember', groupIds: [] })
  created.value = undefined
  emailError.value = ''
  refused.value = undefined
  invite.newKey()
})

const title = computed(() => (created.value ? `Send this link to ${created.value.email}` : 'Invite a member'))

async function submit() {
  emailError.value = ''
  refused.value = undefined
  try {
    created.value = await invite.mutateAsync({
      email: form.email.trim(),
      role: form.role,
      groupIds: form.groupIds,
    })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.field === 'email' || (err.code === 'CONFLICT' && !err.field)) emailError.value = err.message
    else if (err.code === 'ENTITLEMENT_EXCEEDED' || err.code === 'ORG_NOT_ACTIVE') refused.value = err
    else showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" :title="title" width="520px" :close-on-click-modal="!created">
    <el-form v-if="!created" label-position="top" @submit.prevent="submit">
      <el-alert
        v-if="refused"
        type="error"
        :closable="false"
        show-icon
        :title="errorText[refused.code].en"
        :description="refusalDetail(refused)"
        style="margin-bottom: 16px"
      />
      <el-form-item label="Email address" for="inv-email" :error="emailError">
        <el-input
          id="inv-email"
          v-model="form.email"
          type="email"
          placeholder="name@company.com"
          autocomplete="off"
          maxlength="254"
        />
        <div class="t-field-note">They must log in with exactly this address, and it must be verified.</div>
      </el-form-item>
      <el-form-item label="Organization role" for="inv-role">
        <el-radio-group id="inv-role" v-model="form.role">
          <el-radio value="OrgMember">OrgMember</el-radio>
          <el-radio value="OrgAdmin">OrgAdmin</el-radio>
        </el-radio-group>
        <div class="t-field-note">
          OrgAdmin can manage members, groups and Workspaces. OrgMember only sees what their groups can reach.
        </div>
      </el-form-item>
      <el-form-item label="Add to groups (optional)" for="inv-groups">
        <el-select
          id="inv-groups"
          v-model="form.groupIds"
          multiple
          clearable
          placeholder="No group"
          style="width: 100%"
        >
          <el-option v-for="g in groups" :key="g.id" :value="g.id" :label="g.name" />
        </el-select>
      </el-form-item>
    </el-form>

    <div v-else class="t-stack">
      <CopyField :value="created.link ?? ''" label="Copy link" />
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="Copy the link now"
        description="Tenantry does not send email. This link is shown only once, works once, and expires in 7 days."
      />
    </div>

    <template #footer>
      <template v-if="!created">
        <el-button @click="open = false">Cancel</el-button>
        <el-button type="primary" :loading="invite.isPending.value" :disabled="!form.email.trim()" @click="submit"
          >Create invitation link</el-button
        >
      </template>
      <el-button v-else type="primary" @click="open = false">Done</el-button>
    </template>
  </el-dialog>
</template>
