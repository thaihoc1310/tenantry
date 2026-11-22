<script setup lang="ts">
// People who have logged in, and disabling an account (PRODUCT-PLAN §16.5):
// a disabled person loses every portal session at once, and the account is
// turned off in Keycloak a few seconds later.
import { ElMessage, ElMessageBox } from 'element-plus'
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { when } from '@/app/format'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useMe } from '@/features/organizations/queries'
import { syncWords } from '@/features/people/sync'
import { type AdminUser, useSetUserEnabled, useUsers } from './queries'

const route = useRoute()
const me = useMe()
const typed = ref(String(route.query.q ?? ''))
const query = ref(typed.value)
let timer: ReturnType<typeof setTimeout> | undefined
watch(typed, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => (query.value = v.trim()), 300)
})
const users = useUsers(query)
const set = useSetUserEnabled()

async function toggle(u: AdminUser) {
  const disabling = u.status === 'Active'
  const who = u.email ?? u.displayName
  if (disabling) {
    try {
      await ElMessageBox.confirm(
        `${who} is logged out of the portal now and cannot log in again; kubectl and the web console stop within 5 minutes. Their memberships stay.`,
        `Disable ${who}?`,
        { type: 'warning', confirmButtonText: 'Disable account', confirmButtonClass: 'el-button--danger' },
      )
    } catch {
      return
    }
  }
  try {
    await set.mutateAsync({ userId: u.id, enabled: !disabling })
    ElMessage.success(disabling ? `${who} is disabled.` : `${who} can log in again.`)
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell area="admin" active="users">
    <PageHeader title="Users" description="Everyone who has logged in to the portal at least once." />
    <TPanel flush>
      <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
        <el-input
          v-model="typed"
          placeholder="Find by email or name"
          clearable
          style="width: 300px"
          aria-label="Find a user"
        />
      </div>
      <ErrorState v-if="users.isError.value" :error="users.error.value" @retry="users.refetch()" />
      <div v-else class="t-tablewrap">
        <el-table
          v-loading="users.isPending.value"
          :data="users.data.value?.items ?? []"
          style="width: 100%"
          empty-text="Nobody matches."
        >
          <el-table-column label="User" min-width="240">
            <template #default="{ row }"
              ><strong>{{ row.displayName || row.email }}</strong>
              <el-tag v-if="row.id === me.data.value?.id" size="small" type="info" effect="plain" round>You</el-tag>
              <div class="muted">{{ row.email }}</div></template
            >
          </el-table-column>
          <el-table-column label="Account" width="140"
            ><template #default="{ row }"
              ><StatusTag :value="row.status === 'Active' ? 'Active' : 'Blocked'" :label="row.status" /></template
          ></el-table-column>
          <el-table-column label="In Keycloak" width="150">
            <template #default="{ row }">
              <StatusTag v-if="row.syncStatus === 'Synced'" value="Synced" />
              <el-tooltip v-else :content="syncWords(row.syncStatus).why"
                ><span
                  ><StatusTag :tone="syncWords(row.syncStatus).tone" :label="syncWords(row.syncStatus).label" /></span
              ></el-tooltip>
            </template>
          </el-table-column>
          <el-table-column label="First login" width="170"
            ><template #default="{ row }"
              ><span class="num">{{ when(row.createdAt) }}</span></template
            ></el-table-column
          >
          <el-table-column width="120" align="right">
            <template #default="{ row }">
              <el-tooltip v-if="row.id === me.data.value?.id" content="You cannot disable your own account."
                ><span><el-button link disabled>Disable</el-button></span></el-tooltip
              >
              <el-button v-else link :type="row.status === 'Active' ? 'danger' : 'primary'" @click="toggle(row)">{{
                row.status === 'Active' ? 'Disable' : 'Enable'
              }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div v-if="users.data.value?.nextCursor" class="t-panel-foot">
        Showing the first 50. Narrow the search to find someone else.
      </div>
    </TPanel>
  </AppShell>
</template>
