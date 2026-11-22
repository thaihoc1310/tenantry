<script setup lang="ts">
// State as a pill, one vocabulary for every state in the product; busy states spin.
import { computed } from 'vue'

const props = defineProps<{ value?: string; label?: string; tone?: string }>()

const TONE: Record<string, string> = {
  Draft: 'idle',
  Validating: 'busy',
  Reserving: 'busy',
  Provisioning: 'busy',
  Updating: 'busy',
  Deleting: 'busy',
  Ready: 'ok',
  Cordoned: 'warn',
  Degraded: 'warn',
  Failed: 'bad',
  Deleted: 'idle',
  Pending: 'warn',
  Running: 'busy',
  Succeeded: 'ok',
  Blocked: 'bad',
  Active: 'ok',
  Rejected: 'bad',
  Creating: 'busy',
  Synced: 'ok',
  Error: 'bad',
  NotReady: 'bad',
  Maintenance: 'warn',
  Unavailable: 'bad',
  Registered: 'idle',
  Available: 'ok',
  PartiallyAllocated: 'idle',
  FullyAllocated: 'warn',
  Overcommitted: 'bad',
}
const WORDS: Record<string, string> = {
  NotReady: 'Not ready',
  PartiallyAllocated: 'Partly allocated',
  FullyAllocated: 'Fully allocated',
}

const cls = computed(() => {
  const t = props.tone ?? TONE[props.value ?? ''] ?? 'idle'
  return t === 'busy' ? 'info busy' : t
})
const text = computed(() => props.label ?? WORDS[props.value ?? ''] ?? props.value)
</script>

<template>
  <span class="t-status" :class="cls">{{ text }}</span>
</template>
