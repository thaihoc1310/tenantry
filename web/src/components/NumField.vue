<script setup lang="ts">
// A number with its unit inside the box, no stepper arrows.
const model = defineModel<number>({ default: 0 })
withDefaults(
  defineProps<{
    unit?: string
    min?: number
    max?: number
    step?: number
    disabled?: boolean
    id?: string
    label?: string
  }>(),
  { unit: '', min: 0, max: undefined, step: 1, id: undefined, label: undefined },
)
</script>

<template>
  <el-input-number
    :id="id"
    v-model="model"
    class="t-num"
    :aria-label="label"
    :min="min"
    :max="max"
    :step="step"
    :disabled="disabled"
    :controls="false"
  >
    <template v-if="unit" #suffix>{{ unit }}</template>
  </el-input-number>
</template>
