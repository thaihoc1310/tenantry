<script setup lang="ts">
// What will be here, and the next step.
defineProps<{ title: string; description?: string; center?: boolean }>()
</script>

<template>
  <div class="t-empty" :class="{ center }">
    <h2>{{ title }}</h2>
    <p v-if="description">{{ description }}</p>
    <div v-if="$slots.actions" class="t-empty-actions"><slot name="actions"></slot></div>
  </div>
</template>
