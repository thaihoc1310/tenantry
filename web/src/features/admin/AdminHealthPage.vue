<script setup lang="ts">
// Platform health (docs/design/mockups: AdminHealth): the parts the platform
// needs, judged from the database by the API, the work in hand, and the
// shared pools of each cluster.
import { computed } from 'vue'
import { cores } from '@/api/units'
import AppShell from '@/app/AppShell.vue'
import ErrorState from '@/components/ErrorState.vue'
import KeyValues from '@/components/KeyValues.vue'
import PageHeader from '@/components/PageHeader.vue'
import QuotaMeter from '@/components/QuotaMeter.vue'
import StatusTag from '@/components/StatusTag.vue'
import TPanel from '@/components/TPanel.vue'
import { usePlatformHealth } from './queries'

const health = usePlatformHealth()
const warnings = computed(() => health.data.value?.components.filter((c) => c.state === 'warn').length ?? 0)
const work = computed(() => {
  const w = health.data.value?.work
  return w
    ? ([
        ['Operations running', w.operationsRunning],
        ['Operations needing attention', w.operationsNeedingAttention],
        ['Changes waiting for Keycloak', w.syncWaiting],
        ['Requests waiting for review', w.requestsWaiting],
      ] as [string, number][])
    : []
})
</script>

<template>
  <AppShell area="admin" active="health">
    <PageHeader title="Platform health" description="The parts the platform itself needs to work.">
      <template v-if="health.data.value" #tags>
        <StatusTag
          :tone="warnings ? 'warn' : 'ok'"
          :label="warnings ? `${warnings} warning${warnings === 1 ? '' : 's'}` : 'All healthy'"
        />
      </template>
    </PageHeader>
    <ErrorState v-if="health.isError.value" :error="health.error.value" @retry="health.refetch()" />
    <el-skeleton v-else-if="!health.data.value" :rows="6" animated />
    <template v-else>
      <TPanel flush>
        <ul class="t-list">
          <li v-for="c in health.data.value.components" :key="c.name">
            <span class="t-row"
              ><StatusTag :tone="c.state" :label="c.state === 'ok' ? 'Healthy' : 'Warning'" /><strong>{{
                c.name
              }}</strong></span
            >
            <span class="muted">{{ c.detail }}</span>
          </li>
        </ul>
      </TPanel>
      <div class="t-cols">
        <TPanel title="Work in progress">
          <KeyValues :items="work">
            <template #v1="{ value }"
              ><RouterLink to="/admin/operations">{{ value }}</RouterLink></template
            >
            <template #v3="{ value }"
              ><RouterLink to="/admin/organizations">{{ value }}</RouterLink></template
            >
          </KeyValues>
        </TPanel>
        <TPanel v-for="c in health.data.value.clusters" :key="c.id" :title="`Capacity on ${c.displayName}`">
          <div v-if="c.pools.length" class="t-meters">
            <QuotaMeter
              v-for="p in c.pools"
              :key="p.id"
              :label="`${p.name}: CPU reserved`"
              :used="cores(p.allocated.cpu)"
              :total="cores(p.ceiling.cpu)"
              unit="cores"
            />
          </div>
          <p v-else class="muted">No shared pool on this cluster.</p>
        </TPanel>
      </div>
    </template>
  </AppShell>
</template>
