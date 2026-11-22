<script setup lang="ts">
// Organizations by status (docs/design/mockups: AdminOrgs). The review queue
// comes first, oldest request first; an Admin may also create an
// Organization directly.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppShell from '@/app/AppShell.vue'
import { when } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import CreateOrganizationDialog from './CreateOrganizationDialog.vue'
import { type AdminOrganization, type OrgStatus, useAdminOrganizations } from './queries'

const route = useRoute()
const router = useRouter()

const tabs: [string, OrgStatus, string][] = [
  ['pending', 'Pending', 'Waiting for review'],
  ['active', 'Active', 'Active'],
  ['rejected', 'Rejected', 'Rejected'],
]
const tab = computed(() => tabs.find((t) => t[0] === route.query.tab) ?? tabs[0]!)
const list = useAdminOrganizations(() => tab.value[1])
const queue = useAdminOrganizations('Pending', 200)
const waiting = computed(() => queue.items.value.length + (queue.hasNextPage.value ? '+' : ''))

const by = (o: AdminOrganization) => o.owner.email ?? o.owner.displayName
const creating = ref(false)
</script>

<template>
  <AppShell area="admin" active="organizations">
    <PageHeader title="Organizations">
      <template #actions
        ><el-button @click="creating = true"
          ><el-icon><Plus /></el-icon>&nbsp;Create Organization</el-button
        ></template
      >
    </PageHeader>
    <nav class="t-tabs" aria-label="Organizations by status">
      <RouterLink
        v-for="t in tabs"
        :key="t[0]"
        :to="t[0] === 'pending' ? '/admin/organizations' : { path: '/admin/organizations', query: { tab: t[0] } }"
        :aria-current="tab[0] === t[0] ? 'page' : undefined"
        >{{ t[2] }}<template v-if="t[0] === 'pending'"> ({{ waiting }})</template></RouterLink
      >
    </nav>

    <TPanel flush>
      <ErrorState v-if="list.isError.value" :error="list.error.value" @retry="list.refetch()" />
      <div v-else-if="list.isPending.value" style="padding: 20px"><el-skeleton :rows="4" animated /></div>

      <template v-else-if="tab[0] === 'pending'">
        <div v-if="list.items.value.length" class="t-tablewrap">
          <el-table :data="list.items.value" style="width: 100%">
            <el-table-column label="Organization" min-width="160">
              <template #default="{ row }"
                ><strong>{{ row.displayName }}</strong>
                <div class="mono muted">{{ row.slug }}</div></template
              >
            </el-table-column>
            <el-table-column label="Requested by" min-width="180">
              <template #default="{ row }">{{ by(row) }}</template>
            </el-table-column>
            <el-table-column label="Purpose" min-width="280">
              <template #default="{ row }"
                ><span v-if="row.description">{{ row.description }}</span
                ><span v-else class="muted">Not given</span></template
              >
            </el-table-column>
            <el-table-column label="Waiting since" width="170">
              <template #default="{ row }"
                ><span class="num">{{ when(row.createdAt) }}</span></template
              >
            </el-table-column>
            <el-table-column width="110" align="right">
              <template #default="{ row }"
                ><el-button type="primary" size="small" @click="router.push(`/admin/organizations/${row.id}/review`)"
                  >Review</el-button
                ></template
              >
            </el-table-column>
          </el-table>
        </div>
        <EmptyState
          v-else
          center
          title="Nothing to review"
          description="New requests for an Organization appear here."
        />
      </template>

      <template v-else-if="tab[0] === 'active'">
        <div v-if="list.items.value.length" class="t-tablewrap">
          <el-table :data="list.items.value" style="width: 100%">
            <el-table-column label="Organization" min-width="160">
              <template #default="{ row }"
                ><strong>{{ row.displayName }}</strong>
                <div class="mono muted">{{ row.slug }}</div></template
              >
            </el-table-column>
            <el-table-column label="Owner" min-width="190">
              <template #default="{ row }">{{ by(row) }}</template>
            </el-table-column>
            <el-table-column label="Requested" width="170">
              <template #default="{ row }"
                ><span class="num">{{ when(row.createdAt) }}</span></template
              >
            </el-table-column>
            <el-table-column label="Status" width="120">
              <template #default="{ row }"><StatusTag :value="row.status" /></template>
            </el-table-column>
            <el-table-column width="120" align="right">
              <template #default="{ row }"
                ><el-button link type="primary" @click="router.push(`/admin/organizations/${row.id}`)"
                  >Open</el-button
                ></template
              >
            </el-table-column>
          </el-table>
        </div>
        <EmptyState v-else center title="No active Organizations" description="Approved Organizations appear here." />
      </template>

      <template v-else>
        <div v-if="list.items.value.length" class="t-tablewrap">
          <el-table :data="list.items.value" style="width: 100%">
            <el-table-column label="Organization" min-width="150">
              <template #default="{ row }"
                ><strong>{{ row.displayName }}</strong>
                <div class="mono muted">{{ row.slug }} (released)</div></template
              >
            </el-table-column>
            <el-table-column label="Requested by" min-width="180">
              <template #default="{ row }">{{ by(row) }}</template>
            </el-table-column>
            <el-table-column label="Reason given" min-width="300" prop="reviewNote" />
            <el-table-column label="Requested" width="170">
              <template #default="{ row }"
                ><span class="num">{{ when(row.createdAt) }}</span></template
              >
            </el-table-column>
          </el-table>
        </div>
        <EmptyState
          v-else
          center
          title="No rejected requests"
          description="Requests you reject appear here, with the reason you gave."
        />
      </template>

      <div v-if="list.hasNextPage.value" class="t-panel-foot">
        <el-button :loading="list.isFetchingNextPage.value" @click="list.fetchNextPage()">Load more</el-button>
      </div>
    </TPanel>

    <CreateOrganizationDialog v-model="creating" />
  </AppShell>
</template>
