<script setup lang="ts">
// Every change the platform is applying to a cluster (docs/design/mockups:
// AdminOperations). Failed and blocked ones need a decision; forcing a stuck
// deletion comes with Workspace deletion (M7).
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

type Filter = 'needs' | OperationState | ''
const filter = ref<Filter>('needs')
// "Needs attention" is Failed and Blocked together: two lists.
const failed = useAdminOperations('Failed')
const blocked = useAdminOperations('Blocked')
const one = useAdminOperations(() => (filter.value === 'needs' ? 'Succeeded' : filter.value))
const retry = useRetryOperation()

const needs = computed(() => [...blocked.items.value, ...failed.items.value].sort((a, b) => (a.id < b.id ? 1 : -1)))
const rows = computed(() => (filter.value === 'needs' ? needs.value : one.items.value))
const list = computed(() => (filter.value === 'needs' ? failed : one))

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
          <el-radio-button value="needs">Needs attention ({{ needs.length }})</el-radio-button>
          <el-radio-button value="Pending">Pending</el-radio-button>
          <el-radio-button value="Running">Running</el-radio-button>
          <el-radio-button value="Succeeded">Succeeded</el-radio-button>
          <el-radio-button value="">All</el-radio-button>
        </el-radio-group>
      </div>
      <ErrorState v-if="list.isError.value" :error="list.error.value" @retry="list.refetch()" />
      <div v-else class="t-tablewrap">
        <el-table
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
          <el-table-column label="Why" min-width="260">
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
          <el-table-column width="110" align="right">
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
      <div v-if="filter !== 'needs' && one.hasNextPage.value" class="t-panel-foot">
        <el-button :loading="one.isFetchingNextPage.value" @click="one.fetchNextPage()">Load more</el-button>
      </div>
    </TPanel>
  </AppShell>
</template>
