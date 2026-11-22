<script setup lang="ts">
// How to reach a Workspace (docs/design/mockups: Workspace, Connect): its
// web console, and kubectl with a kubeconfig that holds no credential
// (PRODUCT-PLAN §11.2). Access follows the user's groups: someone removed
// from one keeps it until their token expires, within 5 minutes (§9.3).
import { computed } from 'vue'
import KeyValues from '@/components/KeyValues.vue'
import TPanel from '@/components/TPanel.vue'
import { kubeconfigURL, openConsole } from './connect'
import type { Workspace } from './queries'
import { useWorkspaceNamespaces } from './queries'

const props = defineProps<{ ws: Workspace; manages: boolean; usable: boolean; why: string }>()
const namespaces = useWorkspaceNamespaces(() => props.ws.id)
const shown = computed(() => (namespaces.data.value ?? []).filter((n) => n.state === 'Active').slice(0, 3))
const roles = computed(() => {
  const r: string[] = [...(props.ws.myRoles ?? [])]
  if (props.manages) r.push('manager of every Workspace, as owner or admin of the Organization')
  return r.join(', ') || 'none'
})
</script>

<template>
  <el-alert v-if="!usable" type="info" :closable="false" show-icon :title="why" />
  <div class="t-cols">
    <TPanel title="Web console">
      <div class="t-stack">
        <p class="muted">
          Browse and edit workloads in the browser. You are logged in automatically with your Tenantry account.
        </p>
        <div>
          <el-button type="primary" :disabled="!usable" @click="openConsole(ws.id)"
            ><el-icon><Monitor /></el-icon>&nbsp;Open web console</el-button
          >
        </div>
      </div>
    </TPanel>
    <TPanel title="Your access">
      <KeyValues
        :items="[
          ['Role here', roles],
          ['Session', 'renewed every 5 minutes while you are logged in'],
          ['If you lose a role', 'kubectl and the console stop working when your token expires, within 5 minutes'],
        ]"
      />
    </TPanel>
  </div>
  <TPanel title="kubectl">
    <div class="t-stack">
      <p>
        <strong>1. Install the login plugin once.</strong>
        <span class="muted">The kubeconfig contains no password or token; kubectl opens your browser to log in.</span>
      </p>
      <pre class="t-term"><i># macOS or Linux, with Homebrew</i>
<b>brew install</b> kubelogin
<i># or with krew</i>
<b>kubectl krew install</b> oidc-login</pre>
      <p><strong>2. Download the kubeconfig</strong> for this Workspace.</p>
      <div>
        <el-button v-if="usable" tag="a" :href="kubeconfigURL(ws.id)" download
          ><el-icon><Download /></el-icon>&nbsp;Download kubeconfig</el-button
        >
        <el-button v-else disabled
          ><el-icon><Download /></el-icon>&nbsp;Download kubeconfig</el-button
        >
      </div>
      <p><strong>3. Use it.</strong></p>
      <pre class="t-term"><b>export</b> KUBECONFIG=~/Downloads/{{ ws.k8sName }}.kubeconfig
<b>kubectl</b> get namespaces
<i># a browser window opens once; afterwards:</i>
NAME{{ ' '.repeat(Math.max(2, 30 - 4)) }}STATUS
<template v-for="n in shown" :key="n.name">{{ n.name }}{{ ' '.repeat(Math.max(2, 30 - n.name.length)) }}Active
</template></pre>
      <p class="t-field-note">
        You only see the namespaces of Workspaces you have a role in. Creating cluster-wide resources, Ingresses or
        certificates is refused.
      </p>
    </div>
  </TPanel>
</template>
