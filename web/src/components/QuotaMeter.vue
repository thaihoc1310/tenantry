<script setup lang="ts">
// One number with its bar: used of total, what is left. Amber at 85%, red when full.
import { computed } from 'vue'

const props = withDefaults(defineProps<{ label: string; used?: number; total?: number; unit?: string }>(), {
  used: 0,
  total: 0,
  unit: '',
})

const round = (n: number) => Math.round(n * 100) / 100
const left = computed(() => round(props.total - props.used))
const pct = computed(() => (props.total ? Math.min(100, (props.used / props.total) * 100) : 0))
const tone = computed(() => {
  const p = props.total ? props.used / props.total : props.used > 0 ? 2 : 0
  return p > 1 ? 'bad' : p >= 0.85 ? 'warn' : ''
})
const leftText = computed(() =>
  left.value < 0 ? `over by ${-left.value}` : `${left.value}${props.unit === 'Gi' ? ' Gi' : ''} left`,
)
</script>

<template>
  <div class="t-meter">
    <div class="t-meter-top">
      <span>{{ label }}</span
      ><span class="t-meter-left" :class="tone">{{ leftText }}</span>
    </div>
    <div class="t-meter-val">
      {{ used }} <span>of {{ total }}{{ unit ? ' ' + unit : '' }}</span>
    </div>
    <div class="t-track" role="img" :aria-label="`${label}: ${used} of ${total} ${unit} reserved`">
      <i :class="tone" :style="{ width: pct + '%' }"></i>
    </div>
  </div>
</template>
