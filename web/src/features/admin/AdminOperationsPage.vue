<script setup lang="ts">
// Every change the platform is applying to a cluster (docs/design/mockups:
// AdminOperations). Failed and blocked ones need a decision, until a later
// operation on the same target or the target's deletion supersedes them;
// forcing a stuck deletion comes with Workspace deletion (M7).
import { ElMessage } from 'element-plus'
import { computed, ref } from 'vue'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import { when } from '@/app/format'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { type OperationState, useAdminOperations, useRetryOperation } from './queries'

type Filter = 'attention' | OperationState | ''
const filter = ref<Filter>('attention')
// The count on the tab, whichever tab is open.
const needs = useAdminOperations('attention')
const one = useAdminOperations(filter)
const retry = useRetryOperation()
const rows = computed(() => one.items.value)
const list = one

async function doRetry(id: string) {
  try {
    await retry.mutateAsync(id)
    ElMessage.success('Retrying.')
  } catch (err) {
    showFailure(err)
  }
}
</script>

<template>
  <AppShell area="admin" active="operations">
    <PageHeader
      title="Operations"
      description="Every change the platform is applying to a cluster. Blocked and failed ones need a decision."
    />
    <TPanel flush>
      <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
        <el-radio-group v-model="filter" aria-label="Filter operations">
          <el-radio-button value="attention">Needs attention ({{ needs.items.value.length }})</el-radio-button>
          <el-radio-button value="Pending">Pending</el-radio-button>
          <el-radio-button value="Running">Running</el-radio-button>
          <el-radio-button value="Succeeded">Succeeded</el-radio-button>
          <el-radio-button value="">All</el-radio-button>
        </el-radio-group>
      </div>
      <ErrorState v-if="list.isError.value" :error="list.error.value" @retry="list.refetch()" />
      <div v-else class="t-tablewrap">
        <el-table
          scrollbar-always-on
          v-loading="list.isPending.value"
          :data="rows"
          style="width: 100%"
          empty-text="No operation in this state."
        >
          <el-table-column label="Operation" min-width="190">
            <template #default="{ row }"
              ><span class="mono">{{ row.type }}</span>
              <div class="mono muted">{{ row.id }}</div></template
            >
          </el-table-column>
          <el-table-column label="On" min-width="170">
            <template #default="{ row }"
              >{{ row.resourceType }} <span class="mono">{{ row.resourceId }}</span></template
            >
          </el-table-column>
          <el-table-column label="State" width="130"
            ><template #default="{ row }"><StatusTag :value="row.state" /></template
          ></el-table-column>
          <el-table-column label="Why" min-width="220">
            <template #default="{ row }"
              ><span v-if="row.errorMessage"
                >{{ row.errorMessage }} <span class="t-errcode">{{ row.errorCode }}</span></span
              ><span v-else class="muted">—</span></template
            >
          </el-table-column>
          <el-table-column label="Started" width="170"
            ><template #default="{ row }"
              ><span class="num">{{ when(row.createdAt) }}</span></template
            ></el-table-column
          >
          <el-table-column width="110" align="right" fixed="right">
            <template #default="{ row }"
              ><el-button
                v-if="row.state === 'Failed' || row.state === 'Blocked'"
                link
                type="primary"
                @click="doRetry(row.id)"
                >Retry</el-button
              ></template
            >
          </el-table-column>
        </el-table>
      </div>
      <div v-if="one.hasNextPage.value" class="t-panel-foot">
        <el-button :loading="one.isFetchingNextPage.value" @click="one.fetchNextPage()">Load more</el-button>
      </div>
    </TPanel>
  </AppShell>
</template>
