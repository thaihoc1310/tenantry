<script setup lang="ts">
// An Organization's Workspaces (docs/design/mockups: Workspaces): the
// OrgOwner and OrgAdmins see every one, an OrgMember those a group of theirs
// is bound to (architecture/06 §8). The OrgOwner and OrgAdmins create one
// with the wizard (WizardPage).
import { computed } from 'vue'
import { cores, gib } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useOrgDedicatedNodes, useOrganization } from '@/features/organizations/queries'
import { computePlace } from './compute'
import { useWorkspaces } from './queries'

const props = defineProps<{ orgSlug: string }>()
const org = useOrganization(() => props.orgSlug)
const active = computed(() => org.data.value?.status === 'Active')
const list = useWorkspaces(() => props.orgSlug, active)
const manages = computed(() => ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''))
const wizard = computed(() => `/orgs/${props.orgSlug}/workspaces/new`)
// Node names for the Compute column (one cluster in the MVP); only managers
// read the catalog.
const dedicatedNodes = useOrgDedicatedNodes(
  () => props.orgSlug,
  () => (manages.value ? (list.data.value?.find((w) => w.compute.mode === 'Dedicated')?.clusterId ?? '') : ''),
)
</script>

<template>
  <AppShell active="workspaces" :org-slug="orgSlug">
    <PageHeader title="Workspaces" description="Each Workspace is an isolated part of a cluster with its own quota.">
      <template #actions>
        <el-button v-if="manages && active" type="primary" @click="$router.push(wizard)"
          ><el-icon><Plus /></el-icon>&nbsp;Create Workspace</el-button
        >
      </template>
    </PageHeader>
    <ErrorState v-if="list.isError.value" :error="list.error.value" @retry="list.refetch()" />
    <el-skeleton v-else-if="list.isPending.value" :rows="5" animated />
    <TPanel v-else-if="!list.data.value?.length">
      <EmptyState
        v-if="manages"
        title="No Workspaces yet"
        description="A Workspace is an isolated part of the cluster with its own quota, network and members."
      >
        <template #actions>
          <el-button type="primary" @click="$router.push(wizard)">Create your first Workspace</el-button>
        </template>
      </EmptyState>
      <EmptyState
        v-else
        title="No Workspace is shared with you yet"
        description="You see a Workspace once one of your groups gets a role in it. Ask an owner or admin of the Organization."
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
          <el-table-column label="State" width="230"
            ><template #default="{ row }"
              ><StatusTag :value="row.state" /><el-tooltip
                v-if="row.scheduling"
                :content="row.scheduling.message"
                placement="top"
                ><el-tag type="warning" size="small" effect="plain" round style="margin-left: 6px"
                  >{{ row.scheduling.unschedulablePods }} unschedulable</el-tag
                ></el-tooltip
              ></template
            ></el-table-column
          >
          <el-table-column label="Compute" width="200"
            ><template #default="{ row }"
              >{{ row.compute.mode }} <span class="muted">on</span>
              <code>{{ computePlace(row.compute, dedicatedNodes.data.value) }}</code></template
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
