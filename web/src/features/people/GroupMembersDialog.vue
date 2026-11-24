<script setup lang="ts">
// The members of one group: add an Organization member, remove one. Each
// membership shows whether Keycloak has it yet.
import { ElMessage } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { showFailure } from '@/app/failure'
import StatusTag from '@/components/StatusTag.vue'
import { type Group, type Member, useGroupMember } from './queries'
import { syncWords } from './sync'

const props = defineProps<{ orgSlug: string; group: Group; members: Member[] }>()
const open = defineModel<boolean>({ required: true })

const change = useGroupMember(() => props.orgSlug)
const pick = ref('')
watch(open, (o) => {
  if (o) pick.value = ''
})

const inGroup = computed(() => new Set(props.group.members.map((m) => m.userId)))
const candidates = computed(() => props.members.filter((m) => !inGroup.value.has(m.userId)))

async function add() {
  const m = props.members.find((x) => x.userId === pick.value)
  if (!m) return
  try {
    await change.mutateAsync({ groupId: props.group.id, userId: m.userId, add: true })
    pick.value = ''
    ElMessage.success(`${m.displayName} added to ${props.group.name}.`)
  } catch (err) {
    showFailure(err)
  }
}

async function remove(userId: string, name: string) {
  try {
    await change.mutateAsync({ groupId: props.group.id, userId, add: false })
    ElMessage.success(`${name} removed from ${props.group.name}.`)
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" :title="`Members of ${group.name}`" width="620px">
    <div class="t-stack">
      <div class="t-row">
        <el-select
          v-model="pick"
          filterable
          placeholder="Choose a member of the Organization"
          style="flex: 1"
          aria-label="Member to add"
          :no-data-text="'Every member is in this group already.'"
        >
          <el-option
            v-for="m in candidates"
            :key="m.userId"
            :value="m.userId"
            :label="m.email ? `${m.displayName} (${m.email})` : m.displayName"
          />
        </el-select>
        <el-button type="primary" :disabled="!pick" :loading="change.isPending.value" @click="add">Add</el-button>
      </div>

      <div class="t-tablewrap">
        <el-table
          scrollbar-always-on
          :data="group.members"
          style="width: 100%"
          empty-text="Nobody is in this group yet."
        >
          <el-table-column label="Member" min-width="220">
            <template #default="{ row }">
              <strong>{{ row.displayName }}</strong>
              <div class="muted">{{ row.email }}</div>
            </template>
          </el-table-column>
          <el-table-column label="In Keycloak" width="150">
            <template #default="{ row }">
              <StatusTag v-if="row.syncStatus === 'Synced'" value="Synced" />
              <el-tooltip v-else :content="syncWords(row.syncStatus).why"
                ><span
                  ><StatusTag :tone="syncWords(row.syncStatus).tone" :label="syncWords(row.syncStatus).label" /></span
              ></el-tooltip>
            </template>
          </el-table-column>
          <el-table-column width="100" align="right" fixed="right">
            <template #default="{ row }">
              <el-button
                link
                type="danger"
                :disabled="change.isPending.value"
                @click="remove(row.userId, row.displayName)"
                >Remove</el-button
              >
            </template>
          </el-table-column>
        </el-table>
      </div>
      <p class="muted small">
        Someone added must log in again to kubectl or the web console before their token names the group.
      </p>
    </div>
    <template #footer><el-button type="primary" @click="open = false">Done</el-button></template>
  </el-dialog>
</template>
