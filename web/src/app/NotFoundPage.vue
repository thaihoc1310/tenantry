<script setup lang="ts">
import AppShell from '@/app/AppShell.vue'
import EmptyState from '@/components/EmptyState.vue'
import TPanel from '@/components/TPanel.vue'
</script>

<template>
  <AppShell area="bare" narrow>
    <TPanel>
      <EmptyState
        title="No such page"
        description="This address is not part of the portal, or the link is out of date."
      >
        <template #actions><el-button type="primary" @click="$router.push('/')">Go to the portal</el-button></template>
      </EmptyState>
    </TPanel>
  </AppShell>
</template>
