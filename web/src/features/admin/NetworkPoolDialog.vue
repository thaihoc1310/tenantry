<script setup lang="ts">
// Add a network pool to a cluster or change its ranges and block sizes
// (PRODUCT-PLAN §7.1). Ranges may not overlap another pool of the cluster or
// the ranges the cluster uses (CIDR_OVERLAP); only the database changes.
import { ElMessage } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import { showFailure } from '@/app/failure'
import { type NetworkPool, useAllGrants, useCreateNetworkPool, usePatchNetworkPool } from './queries'

const props = defineProps<{ clusterId: string; pool?: NetworkPool }>()
const open = defineModel<boolean>({ required: true })

const create = useCreateNetworkPool()
const patch = usePatchNetworkPool()
const grants = useAllGrants(() => props.clusterId)
const form = reactive({ id: '', orgId: '', cidrs: '', defaultPrefix: 24, allowed: [24] as number[] })
const errors = ref<Record<string, string>>({})
const prefixes = Array.from({ length: 13 }, (_, i) => 16 + i) // /16 … /28

watch(open, (o) => {
  if (!o) return
  const p = props.pool
  Object.assign(form, {
    id: p?.id ?? '',
    orgId: p?.organizationId ?? '',
    cidrs: (p?.cidrs ?? []).join(', '),
    defaultPrefix: p?.defaultPrefixLength ?? 24,
    allowed: [...(p?.allowedPrefixLengths ?? [24])],
  })
  errors.value = {}
  create.newKey()
})
const orgs = computed(() => grants.items.value.map((g) => g.organization))

async function submit() {
  errors.value = {}
  const cidrs = form.cidrs
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  const allowed = [...form.allowed].sort((a, b) => a - b)
  try {
    if (props.pool)
      await patch.mutateAsync({
        clusterId: props.clusterId,
        poolId: props.pool.id,
        body: { cidrs, defaultPrefixLength: form.defaultPrefix, allowedPrefixLengths: allowed },
      })
    else
      await create.mutateAsync({
        clusterId: props.clusterId,
        body: {
          id: form.id.trim(),
          organizationId: form.orgId || undefined,
          cidrs,
          defaultPrefixLength: form.defaultPrefix,
          allowedPrefixLengths: allowed,
        },
      })
    ElMessage.success(props.pool ? 'Pool saved.' : `Pool ${form.id} added.`)
    open.value = false
  } catch (err) {
    if (err instanceof ApiError && err.field) errors.value = { [err.field]: err.message }
    else showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" :title="pool ? `Pool ${pool.id}` : 'Add Pod network pool'" width="600px">
    <el-form label-position="top" @submit.prevent="submit">
      <div class="t-grid-form">
        <el-form-item v-if="!pool" label="Id" for="np-id" :error="errors.id">
          <el-input id="np-id" v-model="form.id" placeholder="acme-pod-network" maxlength="63" />
        </el-form-item>
        <el-form-item v-if="!pool" label="Belongs to" for="np-org" :error="errors.organizationId">
          <el-select id="np-org" v-model="form.orgId" clearable placeholder="Every Organization" style="width: 100%">
            <el-option v-for="o in orgs" :key="o.id" :value="o.id" :label="`${o.displayName} (${o.slug})`" />
          </el-select>
        </el-form-item>
        <el-form-item label="Ranges" for="np-cidrs" :error="errors.cidrs">
          <el-input id="np-cidrs" v-model="form.cidrs" placeholder="10.240.64.0/18" />
          <div class="t-field-note">
            Comma-separated. Adding a range never touches Workspaces that already have one.
          </div>
        </el-form-item>
        <el-form-item label="Block sizes Workspaces may take" for="np-allowed" :error="errors.allowedPrefixLengths">
          <el-select id="np-allowed" v-model="form.allowed" multiple style="width: 100%">
            <el-option v-for="p in prefixes" :key="p" :value="p" :label="`/${p} (${2 ** (32 - p)} addresses)`" />
          </el-select>
        </el-form-item>
        <el-form-item label="Default block size" for="np-default" :error="errors.defaultPrefixLength">
          <el-select id="np-default" v-model="form.defaultPrefix" style="width: 100%">
            <el-option v-for="p in form.allowed" :key="p" :value="p" :label="`/${p}`" />
          </el-select>
        </el-form-item>
      </div>
    </el-form>
    <template #footer>
      <el-button @click="open = false">Cancel</el-button>
      <el-button
        type="primary"
        :loading="create.isPending.value || patch.isPending.value"
        :disabled="!form.cidrs.trim() || !form.allowed.length || (!pool && !form.id.trim())"
        @click="submit"
        >{{ pool ? 'Save pool' : 'Add pool' }}</el-button
      >
    </template>
  </el-dialog>
</template>
