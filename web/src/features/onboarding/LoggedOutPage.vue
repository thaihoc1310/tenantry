<script setup lang="ts">
// After "Log out everywhere": logging in again is a choice, not automatic.
import { login } from '@/api/client'
import EmptyState from '@/components/EmptyState.vue'
import TPanel from '@/components/TPanel.vue'
</script>

<template>
  <div class="t-shell">
    <header class="t-top">
      <span class="t-brand"
        ><span class="t-brand-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>Tenantry</span
      >
    </header>
    <div class="t-bare">
      <div class="t-page narrow">
        <TPanel>
          <EmptyState
            title="You are logged out"
            description="You were logged out of the portal, the web console and kubectl. Log in again to continue."
          >
            <template #actions><el-button type="primary" @click="login('/')">Log in</el-button></template>
          </EmptyState>
        </TPanel>
      </div>
    </div>
  </div>
</template>
