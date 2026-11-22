<script setup lang="ts">
// CPU and memory: requests beside limits, with a note or an error under each.
import NumField from './NumField.vue'

export type Resources = { rcpu: number; lcpu: number; rmem: number; lmem: number }
type Key = keyof Resources

const value = defineModel<Resources>({ required: true })
withDefaults(
  defineProps<{ notes?: Partial<Record<Key, string>>; errors?: Partial<Record<Key, string>>; idp?: string }>(),
  { notes: () => ({}), errors: () => ({}), idp: 'r' },
)

const rows: [string, string, number, Key, Key][] = [
  ['CPU', 'cores', 0.5, 'rcpu', 'lcpu'],
  ['Memory', 'Gi', 1, 'rmem', 'lmem'],
]
</script>

<template>
  <div class="t-res" role="group" aria-label="CPU and memory">
    <span></span>
    <div class="t-res-head"><strong>Requests</strong>guaranteed to Pods</div>
    <div class="t-res-head"><strong>Limits</strong>the most Pods may use</div>
    <template v-for="r in rows" :key="r[0]">
      <div class="t-res-label">{{ r[0] }}</div>
      <div v-for="k in [r[3], r[4]]" :key="k">
        <NumField
          :id="idp + '-' + k"
          v-model="value[k]"
          :label="r[0] + (k[0] === 'r' ? ' requests' : ' limits')"
          :unit="r[1]"
          :step="r[2]"
        />
        <div v-if="errors[k]" class="t-field-note t-field-err">{{ errors[k] }}</div>
        <div v-else-if="notes[k]" class="t-field-note num">{{ notes[k] }}</div>
      </div>
    </template>
  </div>
</template>
