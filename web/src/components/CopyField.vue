<script setup lang="ts">
// A value people paste somewhere else, with a button that copies it.
import { ElMessage } from 'element-plus'
import { ref } from 'vue'

const props = withDefaults(defineProps<{ value: string; label?: string }>(), { label: 'Copy' })
const done = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
  } catch {
    ElMessage.warning('The browser did not allow copying: select the text and copy it yourself.')
    return
  }
  done.value = true
  setTimeout(() => (done.value = false), 1500)
}
</script>

<template>
  <div class="t-copy">
    <span>{{ value }}</span>
    <el-button size="small" @click="copy">{{ done ? 'Copied' : label }}</el-button>
  </div>
</template>
