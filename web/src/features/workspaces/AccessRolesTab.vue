<script setup lang="ts">
// Who can use a Workspace (docs/design/mockups: Workspace, Access roles):
// roles go to groups, never to people (PRODUCT-PLAN §10.3). The OrgOwner and
// OrgAdmins change them; a Workspace keeps at least one TenantOwner group.
import { ElMessage } from 'element-plus'
import { computed, ref } from 'vue'
import type { Schemas } from '@/api/client'
import { showFailure } from '@/app/failure'
import KeyValues from '@/components/KeyValues.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useGroups } from '@/features/people/queries'
import type { Workspace } from './queries'
import { useDeleteBinding, usePutBinding, useWorkspaceBindings } from './queries'

type Role = Schemas['WorkspaceRole']
const roles: Role[] = ['TenantOwner', 'Developer', 'Viewer']

const props = defineProps<{ orgSlug: string; ws: Workspace; manages: boolean; busy: string }>()
const bindings = useWorkspaceBindings(() => props.ws.id)
// Members and sync status are on the groups, which only managers read.
const groups = useGroups(
  () => props.orgSlug,
  () => props.manages,
)
const groupOf = (id: string) => groups.data.value?.find((g) => g.id === id)
const owners = computed(() => (bindings.data.value ?? []).filter((b) => b.role === 'TenantOwner').length)
const lastOwner = (b: Schemas['WorkspaceBinding']) => b.role === 'TenantOwner' && owners.value === 1
const why = computed(() => (!props.manages ? 'Only owners and admins of the Organization change roles.' : props.busy))

const put = usePutBinding()
const del = useDeleteBinding()
// The revocation contract (architecture/06 §5), in the words the UI promises.
const granted = 'Members get it with their next token, within 5 minutes.'
const revoked = 'Kubernetes stops honouring it at once: the role binding is gone.'
async function setRole(b: Schemas['WorkspaceBinding'], role: Role) {
  try {
    await put.mutateAsync({ id: props.ws.id, groupId: b.groupId, role })
    ElMessage.success(`${b.groupName} is ${role} now. ${granted}`)
  } catch (err) {
    showFailure(err)
  }
}
async function remove(b: Schemas['WorkspaceBinding']) {
  try {
    await del.mutateAsync({ id: props.ws.id, groupId: b.groupId })
    ElMessage.success(`${b.groupName} has no role here now. ${revoked}`)
  } catch (err) {
    showFailure(err)
  }
}

// ---- add a group ----
const adding = ref(false)
const pick = ref('')
const role = ref<Role>('Developer')
const unbound = computed(() =>
  (groups.data.value ?? []).filter((g) => !bindings.data.value?.some((b) => b.groupId === g.id)),
)
function openAdd() {
  pick.value = unbound.value.find((g) => g.syncStatus === 'Synced')?.id ?? ''
  role.value = 'Developer'
  adding.value = true
}
async function add() {
  const g = groupOf(pick.value)
  if (!g) return
  try {
    await put.mutateAsync({ id: props.ws.id, groupId: g.id, role: role.value })
    ElMessage.success(`${g.name} is ${role.value} now. ${granted}`)
    adding.value = false
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <TPanel title="Who can use this Workspace" note="roles are given to groups, not to people" flush>
    <template #extra>
      <el-tooltip :content="why" :disabled="!why">
        <span
          ><el-button type="primary" :disabled="!!why" @click="openAdd"
            ><el-icon><Plus /></el-icon>&nbsp;Add group</el-button
          ></span
        >
      </el-tooltip>
    </template>
    <el-skeleton v-if="bindings.isPending.value" :rows="3" animated style="padding: 16px" />
    <div v-else class="t-tablewrap">
      <el-table scrollbar-always-on :data="bindings.data.value ?? []" style="width: 100%">
        <el-table-column label="Group" min-width="170"
          ><template #default="{ row }"
            ><RouterLink v-if="manages" :to="`/orgs/${orgSlug}/groups`"
              ><strong>{{ row.groupName }}</strong></RouterLink
            ><strong v-else>{{ row.groupName }}</strong></template
          ></el-table-column
        >
        <el-table-column label="Role" width="200"
          ><template #default="{ row }"
            ><el-select
              :model-value="row.role"
              size="small"
              style="width: 160px"
              :disabled="!!why"
              :aria-label="`Role of group ${row.groupName}`"
              @update:model-value="(r: Role) => setRole(row, r)"
            >
              <el-option
                v-for="x in roles"
                :key="x"
                :value="x"
                :label="x"
                :disabled="lastOwner(row) && x !== 'TenantOwner'"
              /> </el-select></template
        ></el-table-column>
        <template v-if="manages">
          <el-table-column label="Members" width="110" align="right"
            ><template #default="{ row }"
              ><span class="num">{{ groupOf(row.groupId)?.members.length ?? '' }}</span></template
            ></el-table-column
          >
          <el-table-column label="Sync" width="150"
            ><template #default="{ row }"
              ><StatusTag v-if="groupOf(row.groupId)" :value="groupOf(row.groupId)!.syncStatus" /></template
          ></el-table-column>
        </template>
        <el-table-column width="110" align="right" fixed="right">
          <template #default="{ row }">
            <el-tooltip
              v-if="lastOwner(row)"
              content="A Workspace needs at least one owner group. Add another owner group first."
            >
              <span><el-button link disabled>Remove</el-button></span>
            </el-tooltip>
            <el-button
              v-else
              link
              type="danger"
              :disabled="!!why"
              :aria-label="`Remove ${row.groupName}`"
              @click="remove(row)"
              >Remove</el-button
            >
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="t-panel-foot">
      Removing a group's role here takes effect in Kubernetes at once. Removing a person from a group takes their
      Kubernetes access away within 5 minutes, when their token expires; their portal access goes at once.
    </div>
  </TPanel>
  <TPanel title="What each role can do">
    <KeyValues
      :items="[
        ['TenantOwner', 'Create and delete namespaces and Routes, manage workloads and access inside the Workspace.'],
        ['Developer', 'Deploy and change workloads in existing namespaces.'],
        ['Viewer', 'Read only.'],
      ]"
    />
  </TPanel>

  <el-dialog v-model="adding" title="Add a group to this Workspace" width="460px">
    <el-form label-position="top" @submit.prevent="add">
      <el-form-item label="Group" for="bd-g">
        <el-select id="bd-g" v-model="pick" placeholder="Choose a group" style="width: 100%">
          <el-option
            v-for="g in unbound"
            :key="g.id"
            :value="g.id"
            :label="g.name + (g.syncStatus !== 'Synced' ? '  (sync pending)' : '')"
            :disabled="g.syncStatus !== 'Synced'"
          />
        </el-select>
        <div v-if="!unbound.some((g) => g.syncStatus === 'Synced')" class="t-field-note">
          All synced groups already have a role here.
          <RouterLink :to="`/orgs/${orgSlug}/groups`">Create a group</RouterLink>
        </div>
      </el-form-item>
      <el-form-item label="Role" for="bd-r">
        <el-radio-group id="bd-r" v-model="role">
          <el-radio v-for="x in roles" :key="x" :value="x">{{ x }}</el-radio>
        </el-radio-group>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="adding = false">Cancel</el-button>
      <el-button type="primary" :disabled="!pick" :loading="put.isPending.value" @click="add">Add group</el-button>
    </template>
  </el-dialog>
</template>
