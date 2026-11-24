<script setup lang="ts">
// Every grant of every Organization (docs/design/mockups: AdminGrants).
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import TPanel from '@/components/TPanel.vue'
import { useAllGrants } from './queries'

const router = useRouter()
const grants = useAllGrants()
const q = ref('')
const rows = computed(() =>
  grants.items.value.filter((g) =>
    (g.organization.displayName + ' ' + g.organization.slug).toLowerCase().includes(q.value.toLowerCase()),
  ),
)
const open = (row: (typeof rows.value)[number]) =>
  router.push(`/admin/grants/${row.organization.id}/${row.clusterGrant.clusterId}`)
</script>

<template>
  <AppShell area="admin" active="grants">
    <PageHeader
      title="Grants"
      description="What each Organization may reserve on each cluster. A grant is created when an Organization is approved."
    />
    <TPanel flush>
      <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
        <el-input
          v-model="q"
          placeholder="Find an Organization"
          clearable
          style="width: 260px"
          aria-label="Find an Organization"
        />
      </div>
      <ErrorState v-if="grants.isError.value" :error="grants.error.value" @retry="grants.refetch()" />
      <div v-else class="t-tablewrap">
        <el-table
          scrollbar-always-on
          v-loading="grants.isPending.value"
          :data="rows"
          style="width: 100%"
          row-class-name="t-clickable"
          empty-text="No grant yet. Approve an Organization to create its first grant."
          @row-click="open"
        >
          <el-table-column label="Organization" min-width="160">
            <template #default="{ row }"
              ><strong>{{ row.organization.displayName }}</strong>
              <div class="mono muted">{{ row.organization.slug }}</div></template
            >
          </el-table-column>
          <el-table-column label="Cluster" min-width="120">
            <template #default="{ row }"
              ><code>{{ row.clusterGrant.clusterId }}</code>
              <el-tag v-if="row.clusterGrant.isDefault" size="small" type="info" effect="plain" round
                >Default</el-tag
              ></template
            >
          </el-table-column>
          <el-table-column label="Workspaces" min-width="110">
            <template #default="{ row }"
              ><span class="num"
                >{{ row.usage.workspaces.allocated }} of {{ row.usage.workspaces.limit }}</span
              ></template
            >
          </el-table-column>
          <el-table-column label="Shared CPU" min-width="130">
            <template #default="{ row }"
              ><span class="num"
                >{{ cores(row.usage.requestsCpu.allocated) }} of {{ cores(row.usage.requestsCpu.limit) }}</span
              ></template
            >
          </el-table-column>
          <el-table-column label="Shared memory" min-width="140">
            <template #default="{ row }"
              ><span class="num"
                >{{ gib(row.usage.requestsMemory.allocated) }} of {{ gib(row.usage.requestsMemory.limit) }} Gi</span
              ></template
            >
          </el-table-column>
          <el-table-column label="Shared pools" min-width="160">
            <template #default="{ row }"
              ><code v-for="p in row.clusterGrant.sharedPoolIds" :key="p" style="margin-right: 6px">{{ p }}</code
              ><span v-if="!row.clusterGrant.sharedPoolIds.length" class="muted">Dedicated nodes only</span></template
            >
          </el-table-column>
          <el-table-column width="90" align="right" fixed="right">
            <template #default="{ row }"
              ><el-button link type="primary" @click.stop="open(row)">Edit</el-button></template
            >
          </el-table-column>
        </el-table>
      </div>
      <div v-if="grants.hasNextPage.value" class="t-panel-foot">
        <el-button :loading="grants.isFetchingNextPage.value" @click="grants.fetchNextPage()">Load more</el-button>
      </div>
    </TPanel>
  </AppShell>
</template>
