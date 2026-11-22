<script setup lang="ts">
// The grant an approval gives (docs/design/mockups: GrantForm), plus the
// organization-wide limits. The choices for pools and storage come from the
// onboarding policy's proposal; an Admin may type another id.
import NumField from '@/components/NumField.vue'
import ResourceGrid from '@/components/ResourceGrid.vue'
import type { GrantModel } from './grant'

const g = defineModel<GrantModel>({ required: true })
withDefaults(
  defineProps<{
    errors?: Partial<Record<keyof GrantModel, string>>
    options?: { pools: string[]; nets: string[]; classes: string[] }
    idp?: string
  }>(),
  { errors: () => ({}), options: () => ({ pools: [], nets: [], classes: [] }), idp: 'g' },
)

const limits: [keyof GrantModel, string, string, string][] = [
  ['maxMembers', 'Members', '', ''],
  ['maxGroups', 'Groups', '', 'Includes the 3 default groups.'],
  ['maxWorkspacesTotal', 'Workspaces, all clusters', '', ''],
]
const every: [keyof GrantModel, string, string][] = [
  ['ws', 'Workspaces', ''],
  ['ns', 'Namespaces', ''],
  ['storage', 'Storage', 'Gi'],
]
</script>

<template>
  <el-form label-position="top" class="t-stack" style="gap: 24px" @submit.prevent>
    <section class="t-section">
      <h3>Organization, every cluster</h3>
      <div class="t-fields">
        <el-form-item v-for="f in limits" :key="f[0]" :label="f[1]" :for="idp + '-' + f[0]">
          <NumField
            :id="idp + '-' + f[0]"
            v-model="g[f[0]] as number"
            :unit="f[2]"
            :min="f[0] === 'maxGroups' ? 3 : 0"
          />
          <div v-if="errors[f[0]]" class="t-field-note t-field-err">{{ errors[f[0]] }}</div>
          <div v-else-if="f[3]" class="t-field-note">{{ f[3] }}</div>
        </el-form-item>
      </div>
    </section>
    <section class="t-section">
      <h3>Every Workspace on {{ g.clusterId || 'the cluster' }}, shared or dedicated</h3>
      <div v-if="errors.clusterId" class="t-field-note t-field-err">{{ errors.clusterId }}</div>
      <div class="t-fields">
        <el-form-item v-for="f in every" :key="f[0]" :label="f[1]" :for="idp + '-' + f[0]">
          <NumField :id="idp + '-' + f[0]" v-model="g[f[0]] as number" :unit="f[2]" />
          <div v-if="errors[f[0]]" class="t-field-note t-field-err">{{ errors[f[0]] }}</div>
        </el-form-item>
      </div>
    </section>
    <section class="t-section">
      <h3>Workspaces on a shared pool</h3>
      <div class="t-fields" style="grid-template-columns: minmax(0, 300px)">
        <el-form-item label="Shared pool" :for="idp + '-pool'">
          <el-select
            :id="idp + '-pool'"
            v-model="g.pool"
            clearable
            filterable
            allow-create
            placeholder="None: dedicated nodes only"
            style="width: 100%"
          >
            <el-option v-for="p in options.pools" :key="p" :value="p" :label="p" />
          </el-select>
          <div v-if="errors.pool" class="t-field-note t-field-err">{{ errors.pool }}</div>
        </el-form-item>
      </div>
      <template v-if="g.pool">
        <p class="t-field-note">Shared quota: the total that this Organization's Workspaces may reserve on the pool.</p>
        <ResourceGrid v-model="g" :idp="idp" :errors="errors" />
        <div class="t-fields">
          <el-form-item label="Pods" :for="idp + '-pods'">
            <NumField :id="idp + '-pods'" v-model="g.pods" />
            <div v-if="errors.pods" class="t-field-note t-field-err">{{ errors.pods }}</div>
          </el-form-item>
        </div>
      </template>
    </section>
    <section class="t-section">
      <h3>Workspaces on dedicated nodes</h3>
      <p class="t-explain">
        No quota to set. A dedicated node belongs to the Organization, so its Workspaces are limited by the size of the
        node only. This Organization can use dedicated nodes as soon as you give it one.
      </p>
    </section>
    <section class="t-section">
      <h3>Network and storage</h3>
      <div class="t-fields" style="grid-template-columns: repeat(auto-fill, minmax(220px, 300px))">
        <el-form-item label="Pod network pool" :for="idp + '-net'">
          <el-select :id="idp + '-net'" v-model="g.net" filterable allow-create style="width: 100%">
            <el-option v-for="p in options.nets" :key="p" :value="p" :label="p" />
          </el-select>
          <div v-if="errors.net" class="t-field-note t-field-err">{{ errors.net }}</div>
        </el-form-item>
        <el-form-item label="Storage class" :for="idp + '-sc'">
          <el-select :id="idp + '-sc'" v-model="g.sc" filterable allow-create clearable style="width: 100%">
            <el-option v-for="c in options.classes" :key="c" :value="c" :label="c" />
          </el-select>
          <div v-if="errors.sc" class="t-field-note t-field-err">{{ errors.sc }}</div>
        </el-form-item>
      </div>
    </section>
  </el-form>
</template>
