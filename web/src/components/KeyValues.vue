<script setup lang="ts">
// Facts about one object; slot v<i> replaces the value of item i.
defineProps<{ items: [string, string | number][]; compact?: boolean }>()
</script>

<template>
  <dl class="t-kv" :class="{ compact }">
    <template v-for="(it, i) in items" :key="i">
      <dt>{{ it[0] }}</dt>
      <dd>
        <slot :name="'v' + i" :value="it[1]">{{ it[1] }}</slot>
      </dd>
    </template>
  </dl>
</template>
