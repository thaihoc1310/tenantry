<script setup lang="ts">
// A load that failed: plain words in both languages, the stable code, the request id, retry.
import { computed } from 'vue'
import { ApiError } from '@/api/client'
import { errorText } from '@/i18n/errors'

const props = withDefaults(defineProps<{ error: unknown; title?: string }>(), { title: 'This could not be loaded' })
defineEmits<{ retry: [] }>()

const e = computed(() =>
  props.error instanceof ApiError ? props.error : new ApiError(0, 'INTERNAL', String(props.error ?? '')),
)
const text = computed(() => errorText[e.value.code])
</script>

<template>
  <div class="t-empty">
    <h2>{{ title }}</h2>
    <p>{{ text.en }}</p>
    <p class="muted small" lang="vi">{{ text.vi }}</p>
    <p class="t-errcode">
      {{ e.code }}<template v-if="e.requestId"> · request {{ e.requestId }}</template>
    </p>
    <div class="t-empty-actions"><el-button type="primary" plain @click="$emit('retry')">Try again</el-button></div>
  </div>
</template>
