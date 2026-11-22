<script setup lang="ts">
// The onboarding policy (docs/design/mockups: AdminPolicy): what reviewing a
// request fills in, PRODUCT-PLAN §5.2.1. Saving changes future reviews only.
import { ElMessage } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import AppShell from '@/app/AppShell.vue'
import { showFailure } from '@/app/failure'
import ErrorState from '@/components/ErrorState.vue'
import PageHeader from '@/components/PageHeader.vue'
import TPanel from '@/components/TPanel.vue'
import GrantForm from './GrantForm.vue'
import { emptyGrant, fieldOf, fromApproval, type GrantModel, toApproval } from './grant'
import { useClusters, useNetworkPools, usePolicy, usePutPolicy, useSharedPools } from './queries'

const policy = usePolicy()
const clusters = useClusters()
const put = usePutPolicy()
const grant = ref<GrantModel>(emptyGrant())
const errors = ref<Partial<Record<keyof GrantModel, string>>>({})

const reset = () => {
  grant.value = policy.data.value ? fromApproval(policy.data.value) : emptyGrant()
  errors.value = {}
}
watch(() => policy.data.value, reset, { immediate: true })
const changed = computed(
  () =>
    JSON.stringify(toApproval(grant.value)) !==
    JSON.stringify(policy.data.value ? toApproval(fromApproval(policy.data.value)) : null),
)

// The choices come from the default cluster: its pools, shared ones only.
const pools = useSharedPools(() => grant.value.clusterId)
const nets = useNetworkPools(() => grant.value.clusterId)
const options = computed(() => ({
  pools: (pools.data.value ?? []).map((p) => p.id),
  nets: (nets.data.value ?? []).filter((n) => !n.organizationId).map((n) => n.id),
  classes: grant.value.sc ? [grant.value.sc] : ['standard'],
}))

async function save() {
  errors.value = {}
  try {
    await put.mutateAsync(toApproval(grant.value))
    ElMessage.success('Saved. Reviews from now on start from this policy.')
  } catch (err) {
    if (err instanceof ApiError && fieldOf[err.field]) errors.value = { [fieldOf[err.field]!]: err.message }
    else showFailure(err)
  }
}
</script>

<template>
  <AppShell area="admin" active="policy">
    <PageHeader
      title="Onboarding policy"
      description="The grant that is filled in when you review a request for a new Organization. You can still change it for each request."
    />
    <ErrorState v-if="policy.isError.value" :error="policy.error.value" @retry="policy.refetch()" />
    <el-skeleton v-else-if="policy.isPending.value" :rows="8" animated />
    <TPanel v-else title="Default grant and limits">
      <div class="t-stack">
        <el-alert
          v-if="!policy.data.value"
          type="warning"
          :closable="false"
          show-icon
          title="No onboarding policy is active"
          description="Reviews start from an empty form until you save one."
        />
        <el-form label-position="top" @submit.prevent>
          <el-form-item label="Default cluster of new Organizations" for="op-cluster" style="max-width: 320px">
            <el-select id="op-cluster" v-model="grant.clusterId" style="width: 100%">
              <el-option
                v-for="c in clusters.data.value ?? []"
                :key="c.id"
                :value="c.id"
                :label="`${c.displayName} (${c.status})`"
              />
            </el-select>
            <div v-if="errors.clusterId" class="t-field-note t-field-err">{{ errors.clusterId }}</div>
          </el-form-item>
        </el-form>
        <GrantForm v-model="grant" :errors="errors" :options="options" idp="op" />
        <p class="t-explain">
          Saving changes the starting point for <strong>future</strong> reviews only. Grants that were already given
          stay as they are. Only network pools that belong to every Organization can be in the policy.
        </p>
        <div class="t-row" style="justify-content: flex-end">
          <el-button :disabled="!changed" @click="reset">Discard changes</el-button>
          <el-button
            type="primary"
            :disabled="!changed || !grant.clusterId"
            :loading="put.isPending.value"
            @click="save"
            >Save policy</el-button
          >
        </div>
      </div>
    </TPanel>
  </AppShell>
</template>
