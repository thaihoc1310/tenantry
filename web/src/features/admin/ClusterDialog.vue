<script setup lang="ts">
// Register a cluster, or change one's metadata (PRODUCT-PLAN §16.5). A new
// cluster stays Registered until an Admin marks it Ready.
import { ElMessage } from 'element-plus'
import { reactive, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import { showFailure } from '@/app/failure'
import { type AdminCluster, usePatchCluster, useRegisterCluster } from './queries'

const props = defineProps<{ cluster?: AdminCluster }>() // absent: register a new one
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ registered: [id: string] }>()

const register = useRegisterCluster()
const patch = usePatchCluster()
const form = reactive({ id: '', displayName: '', proxyEndpoint: '', consoleUrl: '', appsDomain: '', reservedCidrs: '' })
const errors = ref<Record<string, string>>({})
const examples = {
  proxy: 'https://kube-b.example.com',
  console: 'https://console-b.example.com',
  apps: 'apps-b.example.com',
  cidrs: '192.168.0.0/16, 10.96.0.0/12',
}

watch(open, (o) => {
  if (!o) return
  const c = props.cluster
  Object.assign(form, {
    id: c?.id ?? '',
    displayName: c?.displayName ?? '',
    proxyEndpoint: c?.proxyEndpoint ?? '',
    consoleUrl: c?.consoleUrl ?? '',
    appsDomain: c?.appsDomain ?? '',
    reservedCidrs: (c?.reservedCidrs ?? []).join(', '),
  })
  errors.value = {}
  register.newKey()
})

async function submit() {
  errors.value = {}
  const cidrs = form.reservedCidrs
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  const body = {
    displayName: form.displayName.trim(),
    proxyEndpoint: form.proxyEndpoint.trim(),
    consoleUrl: form.consoleUrl.trim(),
    appsDomain: form.appsDomain.trim(),
    reservedCidrs: cidrs,
  }
  try {
    if (props.cluster) {
      await patch.mutateAsync({ clusterId: props.cluster.id, patch: body })
      ElMessage.success('Saved.')
    } else {
      const c = await register.mutateAsync({ id: form.id.trim(), ...body })
      ElMessage.success(
        `${c.displayName} is registered. Mark it Ready once Capsule, the proxy and the CRDs are in place.`,
      )
      emit('registered', c.id)
    }
    open.value = false
  } catch (err) {
    if (err instanceof ApiError && err.field) errors.value = { [err.field]: err.message }
    else showFailure(err)
  }
}
</script>

<template>
  <el-dialog v-model="open" :title="cluster ? `Edit ${cluster.displayName}` : 'Register cluster'" width="600px">
    <el-form label-position="top" @submit.prevent="submit">
      <div class="t-grid-form">
        <el-form-item v-if="!cluster" label="Id" for="cl-id" :error="errors.id">
          <el-input id="cl-id" v-model="form.id" placeholder="cluster-b" maxlength="63" />
          <div class="t-field-note">A DNS label; it never changes.</div>
        </el-form-item>
        <el-form-item label="Display name" for="cl-name" :error="errors.displayName">
          <el-input id="cl-name" v-model="form.displayName" maxlength="80" />
        </el-form-item>
        <el-form-item label="kubectl endpoint (Capsule Proxy)" for="cl-proxy" :error="errors.proxyEndpoint">
          <el-input id="cl-proxy" v-model="form.proxyEndpoint" :placeholder="examples.proxy" />
        </el-form-item>
        <el-form-item label="Web console (Headlamp)" for="cl-console" :error="errors.consoleUrl">
          <el-input id="cl-console" v-model="form.consoleUrl" :placeholder="examples.console" />
        </el-form-item>
        <el-form-item label="App domain" for="cl-apps" :error="errors.appsDomain">
          <el-input id="cl-apps" v-model="form.appsDomain" :placeholder="examples.apps" />
        </el-form-item>
        <el-form-item label="Ranges the cluster uses" for="cl-cidrs" :error="errors.reservedCidrs">
          <el-input id="cl-cidrs" v-model="form.reservedCidrs" :placeholder="examples.cidrs" />
          <div class="t-field-note">Pod and Service CIDRs: network pools may not overlap them.</div>
        </el-form-item>
      </div>
    </el-form>
    <template #footer>
      <el-button @click="open = false">Cancel</el-button>
      <el-button type="primary" :loading="register.isPending.value || patch.isPending.value" @click="submit">{{
        cluster ? 'Save' : 'Register cluster'
      }}</el-button>
    </template>
  </el-dialog>
</template>
