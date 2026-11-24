<script setup lang="ts">
// An Organization's audit trail (docs/design/mockups: Audit), for its OrgOwner
// and OrgAdmins: every change made through the portal, newest first, refused
// ones too. The filters narrow the pages loaded so far.
import { computed, ref } from 'vue'
import type { Schemas } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { when } from '@/app/format'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { useAuditEvents, useOrganization } from './queries'

const props = defineProps<{ orgSlug: string }>()
const org = useOrganization(() => props.orgSlug)
const allowed = computed(
  () => org.data.value?.status === 'Active' && ['OrgOwner', 'OrgAdmin'].includes(org.data.value?.myRole ?? ''),
)
const events = useAuditEvents(() => props.orgSlug, allowed)

type Event = Schemas['AuditEvent']
const who = (e: Event) => e.actor?.email ?? e.actor?.displayName ?? 'The platform'
const kind = (e: Event) => e.action.split('.')[0] ?? e.action
// What the change was on, in the words the person typed where the detail has them.
function on(e: Event): string {
  const d = e.detail as Record<string, unknown>
  for (const k of ['slug', 'name', 'email', 'clusterId']) if (typeof d[k] === 'string' && d[k]) return String(d[k])
  return e.targetId
}

const actor = ref('')
const action = ref('')
const actors = computed(() => [...new Set(events.items.value.map(who))].sort())
const kinds = computed(() => [...new Set(events.items.value.map(kind))].sort())
const rows = computed(() =>
  events.items.value.filter(
    (e) => (!actor.value || who(e) === actor.value) && (!action.value || kind(e) === action.value),
  ),
)
</script>

<template>
  <AppShell active="audit" :org-slug="orgSlug">
    <PageHeader title="Audit" description="Every change made to this Organization through the portal, newest first." />
    <ErrorState v-if="org.isError.value" :error="org.error.value" @retry="org.refetch()" />
    <el-skeleton v-else-if="org.isPending.value" :rows="6" animated />
    <TPanel v-else-if="!allowed">
      <EmptyState
        title="Only owners and admins see the audit trail"
        description="Ask the owner or an admin of the Organization."
      />
    </TPanel>
    <ErrorState v-else-if="events.isError.value" :error="events.error.value" @retry="events.refetch()" />
    <TPanel v-else flush>
      <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
        <el-select v-model="actor" placeholder="Anyone" clearable style="width: 240px" aria-label="Filter by person">
          <el-option v-for="a in actors" :key="a" :value="a" :label="a" />
        </el-select>
        <el-select
          v-model="action"
          placeholder="Any action"
          clearable
          style="width: 240px"
          aria-label="Filter by action"
        >
          <el-option v-for="a in kinds" :key="a" :value="a" :label="a + '.*'" />
        </el-select>
      </div>
      <el-skeleton v-if="events.isPending.value" :rows="5" animated style="padding: 16px" />
      <div v-else class="t-tablewrap">
        <el-table scrollbar-always-on :data="rows" style="width: 100%" empty-text="No change matches these filters.">
          <el-table-column label="When" width="180"
            ><template #default="{ row }"
              ><span class="num">{{ when(row.at) }}</span></template
            ></el-table-column
          >
          <el-table-column label="Who" min-width="200"
            ><template #default="{ row }">{{ who(row) }}</template></el-table-column
          >
          <el-table-column label="Action" min-width="210"
            ><template #default="{ row }"
              ><span class="mono">{{ row.action }}</span></template
            ></el-table-column
          >
          <el-table-column label="On" min-width="200"
            ><template #default="{ row }"
              ><span class="muted small">{{ row.targetType }}</span> {{ on(row) }}</template
            ></el-table-column
          >
          <el-table-column label="Result" width="130"
            ><template #default="{ row }"><StatusTag :value="row.result" /></template
          ></el-table-column>
        </el-table>
      </div>
      <div class="t-panel-foot t-row t-between">
        <span
          >Showing {{ rows.length }} of {{ events.items.value.length }} loaded. Changes made with kubectl are recorded
          by the cluster, not here.</span
        >
        <el-button
          v-if="events.hasNextPage.value"
          size="small"
          :loading="events.isFetchingNextPage.value"
          @click="events.fetchNextPage()"
          >Load older</el-button
        >
      </div>
    </TPanel>
  </AppShell>
</template>
