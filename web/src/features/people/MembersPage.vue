<script setup lang="ts">
// Members and open invitations of an Organization (docs/design/mockups:
// Members), for its OrgOwner and OrgAdmins (PRODUCT-PLAN §16.2).
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, ref } from 'vue'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { expiresIn, when } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import TPanel from '@/components/TPanel.vue'
import { useMe, useOrganization, useUsage } from '@/features/organizations/queries'
import InviteDialog from './InviteDialog.vue'
import {
  type InvitationRole,
  type Member,
  useGroups,
  useInvitations,
  useMembers,
  useRemoveMember,
  useRevokeInvitation,
  useSetRole,
} from './queries'

const props = defineProps<{ orgSlug: string }>()
const slug = () => props.orgSlug

const me = useMe()
const org = useOrganization(slug)
const manages = computed(() => ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''))
const members = useMembers(slug, manages)
const invitations = useInvitations(slug)
const groups = useGroups(slug, manages)
const usage = useUsage(slug, manages)
const setRole = useSetRole(slug)
const remove = useRemoveMember(slug)
const revoke = useRevokeInvitation(slug)

const limit = computed(() => usage.data.value?.organization.members.limit)
const full = computed(() => limit.value !== undefined && (members.data.value?.length ?? 0) >= limit.value)
const groupName = computed(() => new Map((groups.data.value ?? []).map((g) => [g.id, g.name])))
const description = computed(() => {
  const n = members.data.value?.length ?? 0
  const of = limit.value === undefined ? '' : ` of ${limit.value}`
  return `${n}${of} members. What a member can do inside a Workspace comes from their groups.`
})

const inviting = ref(false)

async function changeRole(m: Member, role: InvitationRole) {
  try {
    await setRole.mutateAsync({ userId: m.userId, role })
    ElMessage.success(`${m.displayName} is now ${role}.`)
  } catch (err) {
    showFailure(err)
  }
}

async function removeMember(m: Member) {
  try {
    await ElMessageBox.confirm(
      `${m.displayName} loses access to ${org.data.value?.displayName} in the portal now, and in Kubernetes within 5 minutes. They leave all of its groups.`,
      `Remove ${m.displayName}?`,
      { type: 'warning', confirmButtonText: 'Remove member', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return // cancelled
  }
  try {
    await remove.mutateAsync(m.userId)
    ElMessage.success(`${m.displayName} was removed.`)
  } catch (err) {
    showFailure(err)
  }
}

async function revokeInvitation(id: string, email: string) {
  try {
    await revoke.mutateAsync(id)
    ElMessage.success(`The invitation for ${email} no longer works.`)
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell active="members" :org-slug="orgSlug">
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="!org.data.value" :rows="8" animated />
    <TPanel v-else-if="!manages">
      <EmptyState
        title="Only the owner and admins manage members"
        description="Ask an OrgOwner or OrgAdmin of this Organization to invite someone or change a role."
      />
    </TPanel>

    <template v-else>
      <PageHeader title="Members" :description="description">
        <template #actions>
          <el-tooltip
            :disabled="!full"
            content="The Organization has as many members as it may. Remove a member or ask a Platform Admin for a higher limit."
            placement="bottom-end"
          >
            <span
              ><el-button type="primary" :disabled="full" @click="inviting = true"
                ><el-icon><Plus /></el-icon>&nbsp;Invite member</el-button
              ></span
            >
          </el-tooltip>
        </template>
      </PageHeader>

      <TPanel flush>
        <ErrorState v-if="members.isError.value" :error="members.error.value" @retry="members.refetch()" />
        <div v-else class="t-tablewrap">
          <el-table
            scrollbar-always-on
            v-loading="members.isPending.value"
            :data="members.data.value ?? []"
            style="width: 100%"
          >
            <el-table-column label="Member" min-width="220">
              <template #default="{ row }">
                <strong>{{ row.displayName }}</strong>
                <el-tag v-if="row.userId === me.data.value?.id" size="small" type="info" effect="plain" round
                  >You</el-tag
                >
                <div class="muted">{{ row.email }}</div>
              </template>
            </el-table-column>
            <el-table-column label="Organization role" width="200">
              <template #default="{ row }">
                <span v-if="row.role === 'OrgOwner'">OrgOwner</span>
                <el-select
                  v-else
                  :model-value="row.role"
                  size="small"
                  style="width: 150px"
                  :aria-label="'Role of ' + row.displayName"
                  :disabled="setRole.isPending.value"
                  @update:model-value="(r: InvitationRole) => changeRole(row, r)"
                >
                  <el-option value="OrgAdmin" label="OrgAdmin" />
                  <el-option value="OrgMember" label="OrgMember" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column label="Groups" min-width="200">
              <template #default="{ row }">
                <div v-if="row.groups.length" class="t-row" style="gap: 6px">
                  <el-tag v-for="g in row.groups" :key="g.id" type="info" effect="plain">{{ g.name }}</el-tag>
                </div>
                <span v-else class="muted">No group</span>
              </template>
            </el-table-column>
            <el-table-column label="Joined" width="170">
              <template #default="{ row }"
                ><span class="num">{{ when(row.joinedAt) }}</span></template
              >
            </el-table-column>
            <el-table-column width="110" align="right" fixed="right">
              <template #default="{ row }">
                <el-tooltip
                  v-if="row.role === 'OrgOwner'"
                  content="The owner cannot be removed. Transfer ownership first, in Settings."
                  ><span><el-button link disabled>Remove</el-button></span></el-tooltip
                >
                <el-tooltip
                  v-else-if="row.userId === me.data.value?.id"
                  content="To leave the Organization yourself, use Settings."
                  ><span><el-button link disabled>Remove</el-button></span></el-tooltip
                >
                <el-button v-else link type="danger" @click="removeMember(row)">Remove</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <TPanel
        title="Open invitations"
        flush
        foot="The link is shown once, when the invitation is created. To send it again, invite the same address again: the old link stops working."
      >
        <ErrorState v-if="invitations.isError.value" :error="invitations.error.value" @retry="invitations.refetch()" />
        <div v-else class="t-tablewrap">
          <el-table
            scrollbar-always-on
            v-loading="invitations.isPending.value"
            :data="invitations.data.value ?? []"
            style="width: 100%"
            empty-text="No open invitations."
          >
            <el-table-column label="Email" min-width="220" prop="email" />
            <el-table-column label="Role" width="130" prop="role" />
            <el-table-column label="Groups" min-width="160">
              <template #default="{ row }">
                <div v-if="row.groupIds.length" class="t-row" style="gap: 6px">
                  <el-tag v-for="id in row.groupIds" :key="id" type="info" effect="plain">{{
                    groupName.get(id) ?? 'deleted group'
                  }}</el-tag>
                </div>
                <span v-else class="muted">None</span>
              </template>
            </el-table-column>
            <el-table-column label="Invited by" min-width="150">
              <template #default="{ row }">{{ row.invitedBy.displayName }}</template>
            </el-table-column>
            <el-table-column label="Expires" width="120">
              <template #default="{ row }">
                <span :class="{ muted: expiresIn(row.expiresAt) === 'expired' }" :title="when(row.expiresAt)">{{
                  expiresIn(row.expiresAt)
                }}</span>
              </template>
            </el-table-column>
            <el-table-column width="110" align="right" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" @click="revokeInvitation(row.id, row.email)">Revoke</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <InviteDialog v-model="inviting" :org-slug="orgSlug" :groups="groups.data.value ?? []" />
    </template>
  </AppShell>
</template>
