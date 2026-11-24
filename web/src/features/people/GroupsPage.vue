<script setup lang="ts">
// Groups of an Organization (docs/design/mockups: Groups), for its OrgOwner
// and OrgAdmins. A group reaches tokens once Keycloak has it: its sync status
// says whether it has (IMPLEMENTATION-PLAN §5.2).
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useOrganization, useUsage } from '@/features/organizations/queries'
import { errorText, refusalDetail } from '@/i18n/errors'
import GroupMembersDialog from './GroupMembersDialog.vue'
import { type Group, useCreateGroup, useDeleteGroup, useGroups, useMembers, useRenameGroup } from './queries'
import { syncWords } from './sync'

const props = defineProps<{ orgSlug: string }>()
const slug = () => props.orgSlug

const org = useOrganization(slug)
const manages = computed(() => ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''))
const groups = useGroups(slug, manages)
const members = useMembers(slug, manages)
const usage = useUsage(slug, manages)
const create = useCreateGroup(slug)
const rename = useRenameGroup(slug)
const del = useDeleteGroup(slug)

const limit = computed(() => usage.data.value?.organization.groups.limit)
const full = computed(() => limit.value !== undefined && (groups.data.value?.length ?? 0) >= limit.value)
const description = computed(() => {
  const n = groups.data.value?.length ?? 0
  const of = limit.value === undefined ? '' : ` of ${limit.value}`
  return `${n}${of} groups. A group gets a role in a Workspace; its members get that role in Kubernetes.`
})

// The members dialog follows the group as the list refreshes.
const openGroupId = ref('')
const openGroup = computed(() => groups.data.value?.find((g) => g.id === openGroupId.value))
const membersOpen = computed({
  get: () => !!openGroup.value,
  set: (v) => {
    if (!v) openGroupId.value = ''
  },
})

const creating = ref(false)
const name = ref('')
const nameError = ref('')
const refused = ref<ApiError>()
watch(creating, (o) => {
  if (!o) return
  name.value = ''
  nameError.value = ''
  refused.value = undefined
  create.newKey()
})

async function submitCreate() {
  nameError.value = ''
  refused.value = undefined
  try {
    const g = await create.mutateAsync(name.value.trim())
    creating.value = false
    ElMessage.success(`Group ${g.name} created. It reaches Keycloak in a few seconds.`)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.field === 'name') nameError.value = err.message
    else if (err.code === 'ENTITLEMENT_EXCEEDED' || err.code === 'ORG_NOT_ACTIVE') refused.value = err
    else showFailure(err)
  }
}

async function renameGroup(g: Group) {
  let value: string
  try {
    ;({ value } = await ElMessageBox.prompt('Members and admins see this name.', `Rename ${g.name}`, {
      inputValue: g.name,
      confirmButtonText: 'Rename',
      inputValidator: (v) => (v.trim() ? true : 'Enter a name'),
    }))
  } catch {
    return
  }
  if (value.trim() === g.name) return
  try {
    await rename.mutateAsync({ groupId: g.id, name: value.trim() })
    ElMessage.success(`Renamed to ${value.trim()}.`)
  } catch (err) {
    showFailure(err)
  }
}

async function deleteGroup(g: Group) {
  try {
    await ElMessageBox.confirm(
      `Its ${g.members.length} member(s) leave the group, and the group leaves Keycloak within a few seconds.`,
      `Delete ${g.name}?`,
      { type: 'warning', confirmButtonText: 'Delete group', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  try {
    await del.mutateAsync(g.id)
    ElMessage.success(`Group ${g.name} deleted.`)
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell active="groups" :org-slug="orgSlug">
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="!org.data.value" :rows="8" animated />
    <TPanel v-else-if="!manages">
      <EmptyState
        title="Only the owner and admins manage groups"
        description="Ask an OrgOwner or OrgAdmin of this Organization to add you to a group."
      />
    </TPanel>

    <template v-else>
      <PageHeader title="Groups" :description="description">
        <template #actions>
          <el-tooltip
            :disabled="!full"
            content="The Organization has as many groups as it may. Delete a group or ask a Platform Admin for a higher limit."
            placement="bottom-end"
          >
            <span
              ><el-button type="primary" :disabled="full" @click="creating = true"
                ><el-icon><Plus /></el-icon>&nbsp;Create group</el-button
              ></span
            >
          </el-tooltip>
        </template>
      </PageHeader>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="Changes reach Kubernetes within a few minutes"
        description="After you add someone to a group they must log in again to kubectl or the web console. After you remove someone, their Kubernetes access ends within 5 minutes."
      />

      <TPanel flush>
        <ErrorState v-if="groups.isError.value" :error="groups.error.value" @retry="groups.refetch()" />
        <div v-else class="t-tablewrap">
          <el-table
            scrollbar-always-on
            v-loading="groups.isPending.value"
            :data="groups.data.value ?? []"
            style="width: 100%"
            empty-text="No groups."
          >
            <el-table-column label="Group" min-width="170">
              <template #default="{ row }"
                ><strong>{{ row.name }}</strong></template
              >
            </el-table-column>
            <el-table-column label="Members" width="110" align="right">
              <template #default="{ row }"
                ><span class="num">{{ row.members.length }}</span></template
              >
            </el-table-column>
            <el-table-column label="Roles in Workspaces" min-width="200">
              <template #default><span class="muted">Not used in any Workspace</span></template>
            </el-table-column>
            <el-table-column label="Sync to login system" width="190">
              <template #default="{ row }">
                <StatusTag v-if="row.syncStatus === 'Synced'" value="Synced" />
                <el-tooltip v-else :content="syncWords(row.syncStatus).why"
                  ><span
                    ><StatusTag :tone="syncWords(row.syncStatus).tone" :label="syncWords(row.syncStatus).label" /></span
                ></el-tooltip>
              </template>
            </el-table-column>
            <el-table-column width="220" align="right" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openGroupId = row.id">Members</el-button>
                <el-button link @click="renameGroup(row)">Rename</el-button>
                <el-button link type="danger" @click="deleteGroup(row)">Delete</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </TPanel>

      <el-dialog v-model="creating" title="Create group" width="460px">
        <el-form label-position="top" @submit.prevent="submitCreate">
          <el-alert
            v-if="refused"
            type="error"
            :closable="false"
            show-icon
            :title="errorText[refused.code].en"
            :description="refusalDetail(refused)"
            style="margin-bottom: 16px"
          />
          <el-form-item label="Name" for="grp-name" :error="nameError">
            <el-input id="grp-name" v-model="name" placeholder="qa" maxlength="63" />
            <div class="t-field-note">
              Members see this name. Kubernetes and Keycloak use a fixed internal name, so renaming later is safe.
            </div>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="creating = false">Cancel</el-button>
          <el-button type="primary" :loading="create.isPending.value" :disabled="!name.trim()" @click="submitCreate"
            >Create group</el-button
          >
        </template>
      </el-dialog>

      <GroupMembersDialog
        v-if="openGroup"
        v-model="membersOpen"
        :org-slug="orgSlug"
        :group="openGroup"
        :members="members.data.value ?? []"
      />
    </template>
  </AppShell>
</template>
