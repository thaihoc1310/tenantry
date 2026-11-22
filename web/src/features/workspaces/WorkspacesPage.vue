<script setup lang="ts">
// An Organization's Workspaces (docs/design/mockups: Workspaces): the
// OrgOwner and OrgAdmins see every one, an OrgMember those a group of theirs
// is bound to (architecture/06 §8). Creating one is the wizard's (M10).
import { computed } from 'vue'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useOrganization } from '@/features/organizations/queries'
import { useWorkspaces } from './queries'

const props = defineProps<{ orgSlug: string }>()
const org = useOrganization(() => props.orgSlug)
const active = computed(() => org.data.value?.status === 'Active')
const list = useWorkspaces(() => props.orgSlug, active)
</script>

<template>
  <AppShell active="workspaces" :org-slug="orgSlug">
    <PageHeader title="Workspaces" description="Each Workspace is an isolated part of a cluster with its own quota." />
    <ErrorState v-if="list.isError.value" :error="list.error.value" @retry="list.refetch()" />
    <el-skeleton v-else-if="list.isPending.value" :rows="5" animated />
    <TPanel v-else-if="!list.data.value?.length">
      <EmptyState
        title="No Workspace you can reach yet"
        description="Workspaces you or your groups have a role in appear here."
      />
    </TPanel>
    <TPanel v-else flush>
      <div class="t-tablewrap">
        <el-table :data="list.data.value" style="width: 100%">
          <el-table-column label="Workspace" min-width="200"
            ><template #default="{ row }"
              ><RouterLink :to="`/orgs/${orgSlug}/workspaces/${row.slug}`"
                ><strong>{{ row.displayName }}</strong></RouterLink
              >
              <div class="mono muted small">{{ row.k8sName }}</div></template
            ></el-table-column
          >
          <el-table-column label="State" width="150"
            ><template #default="{ row }"><StatusTag :value="row.state" /></template
          ></el-table-column>
          <el-table-column label="Compute" width="200"
            ><template #default="{ row }"
              >{{ row.compute.mode }} <span class="muted">on</span>
              <code>{{ row.compute.sharedPoolId ?? row.compute.dedicatedNodeGrantId }}</code></template
            ></el-table-column
          >
          <el-table-column label="Requests" width="170" align="right"
            ><template #default="{ row }"
              ><span class="num"
                >{{ cores(row.resources.requestsCpu) }} cores, {{ gib(row.resources.requestsMemory) }} Gi</span
              ></template
            ></el-table-column
          >
          <el-table-column label="Cluster" width="120" prop="clusterId" />
        </el-table>
      </div>
    </TPanel>
  </AppShell>
</template>
