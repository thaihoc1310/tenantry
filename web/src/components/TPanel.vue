<script setup lang="ts">
// A titled block of content; #extra sits right of the title. Do not nest panels.
defineProps<{ title?: string; note?: string; flush?: boolean; danger?: boolean; foot?: string }>()
</script>

<template>
  <section class="t-panel" :class="{ 't-danger': danger }">
    <div v-if="title || $slots.extra" class="t-panel-head">
      <div class="t-row">
        <h2>{{ title }}</h2>
        <span v-if="note" class="muted">{{ note }}</span>
      </div>
      <div class="t-row"><slot name="extra"></slot></div>
    </div>
    <div class="t-panel-body" :class="{ flush }"><slot></slot></div>
    <div v-if="foot" class="t-panel-foot">{{ foot }}</div>
  </section>
</template>
