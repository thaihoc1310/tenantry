<script setup lang="ts">
// Where you are, what it is, its state, the main action.
import type { RouteLocationRaw } from 'vue-router'

withDefaults(
  defineProps<{ title: string; crumbs?: { label: string; to?: RouteLocationRaw }[]; description?: string }>(),
  {
    crumbs: () => [],
    description: '',
  },
)
</script>

<template>
  <header class="t-head">
    <div class="t-head-main">
      <nav v-if="crumbs.length" class="t-crumbs" aria-label="Breadcrumb">
        <template v-for="(c, i) in crumbs" :key="i">
          <RouterLink v-if="c.to" :to="c.to">{{ c.label }}</RouterLink
          ><span v-else>{{ c.label }}</span>
          <span v-if="i < crumbs.length - 1" aria-hidden="true">/</span>
        </template>
      </nav>
      <div class="t-head-title">
        <h1>{{ title }}</h1>
        <slot name="tags"></slot>
      </div>
      <p v-if="description" class="t-head-desc">{{ description }}</p>
    </div>
    <div class="t-head-actions"><slot name="actions"></slot></div>
  </header>
</template>
